package main

import (
	"context"
	"log"
	"math/rand"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/fiatjaf/eventstore/badger"
	"github.com/fiatjaf/khatru"
	"github.com/fiatjaf/relay29"
	"github.com/nbd-wtf/go-nostr"
)

// The relay IS the conductor. It is the only writer of now_playing (kind 30100) + the play-log
// (kind 1313): the single always-on time authority. A club plays its DJs' queues round-robin
// autonomously — no client needs to be in the foreground, the phone can be locked, a DJ's net
// can drop. Clients become pure consumers (read now_playing, drift-correct, play) and request
// changes (queue 30103, stage 30102, skip 30107).
//
// Why server-side: the previous client-conductor had to simulate an always-on authority with
// intermittent browsers (election, failover, rescue, sticky stage) — fragile and the source of
// the round-robin divergence bugs. Moving the single writer to the always-on relay deletes that
// whole class of problem. relay29 already lets the relay's own key write to any group
// (event_policy.go: `event.PubKey == s.publicKey`). We bypass the RejectEvent chain entirely
// and write straight to the store — the relay's signed events are trusted and need no rate-limit
// / membership / PoW gating.

// condPrem is set by main.go after premiumStore is initialized.
// The conductor uses it to determine per-club DJ slot limits.
var condPrem *premiumStore

const (
	condMaxDJs            = 5            // absolute max (premium clubs)
	condMaxDJsFree        = 2            // non-premium clubs
	condMaxDJsOrig        = 5            // kept for reference
	condStageStaleMS      = 3_600_000    // sticky stage: 1h after last heartbeat (STALE_MS)
	condOnlineMS          = 50_000       // a DJ is "present" within this of their last 20100 beat
	condHeartbeatMS       = 15_000       // now_playing republish cadence (latecomers + drift)
	condMaxTrackFallbackS = 600          // duration<=0 → cap so a missing-duration track ends
	condTickMS            = 2500         // scheduler granularity (precise enough track-end)
	kindNowPlaying        = 30100
	kindStage             = 30102
	kindQueue             = 30103
	kindStageKick         = 30106
	kindSkip              = 30107
	kindAutoDJ            = 30105 // owner-authored: arm/disarm auto-dj playlist for a club
	kindAutoDJCtrl        = 30111 // relay-signed: disarm marker (real DJ took over)
	kindBroken            = 20102 // ephemeral "I can't play this track" report (content = videoId)
	kindPlay              = 1313
	brokenWindowMS        = 120_000 // a broken-track report counts as fresh this long
	brokenQuorum          = 2       // distinct members reporting a track broken → skip it
)

type condDJ struct {
	pubkey string
	since  int64
}

type condTrack struct {
	videoID  string
	title    string
	duration int
	active   bool
}

// per-club authoritative playback state (in-memory; rebuilt from the store on cold start).
type condClub struct {
	pos        int
	videoID    string
	dj         string
	title      string
	duration   int
	startedAt  int64 // ms (relay clock)
	lastBeat   int64 // ms of the last now_playing publish
	playing    bool
	inTakeover bool // true while a live-session (kind 30109, mode=takeover) is active
	// Auto DJ state (zero-value = not initialized; reinit on playlist-length change).
	autoOrder []int
	autoIdx   int
}

// autoState carries the owner + parsed tracks for a club in Auto DJ mode.
type autoState struct {
	owner  string
	tracks []condTrack
}

type conductor struct {
	db    *badger.BadgerBackend
	relay *khatru.Relay
	state *relay29.State // group membership/roles (skip authorization)
	sk    string
	pub   string
	mu    sync.Mutex
	clubs map[string]*condClub

	presMu sync.Mutex
	pres   map[string]map[string]int64 // club → pubkey → last presence beat (ms)

	brokenMu sync.Mutex
	broken   map[string]map[string]map[string]int64 // club → videoId → reporter → ts (ms)
}

func newConductor(db *badger.BadgerBackend, relay *khatru.Relay, state *relay29.State, sk string) *conductor {
	pub, _ := nostr.GetPublicKey(sk)
	return &conductor{db: db, relay: relay, state: state, sk: sk, pub: pub, clubs: map[string]*condClub{}, pres: map[string]map[string]int64{}, broken: map[string]map[string]map[string]int64{}}
}

// clubOwner returns the pubkey of the club's creator (author of its 9007 create-group event).
func (c *conductor) clubOwner(ctx context.Context, club string) string {
	ch, err := c.db.QueryEvents(ctx, nostr.Filter{
		Kinds: []int{kindCreateGroup},
		Tags:  nostr.TagMap{"h": []string{club}},
	})
	if err != nil {
		return ""
	}
	var newest *nostr.Event
	for ev := range ch {
		if newest == nil || ev.CreatedAt > newest.CreatedAt {
			newest = ev
		}
	}
	if newest == nil {
		return ""
	}
	return newest.PubKey
}

// observeBroken records an ephemeral "I can't play this track" report (kind 20102, content =
// videoId). Registered on OnEphemeralEvent (like presence). The conductor skips the running
// track when an AUTHORIZED reporter (owner/mod/playing-DJ) reports it broken, OR when a quorum
// of distinct members do — fixing dead/region-locked videos that the relay can't detect itself,
// without reopening skip to single-member abuse.
func (c *conductor) observeBroken(_ context.Context, ev *nostr.Event) {
	if ev.Kind != kindBroken {
		return
	}
	club, vid := tagVal(ev, "h"), ev.Content
	if club == "" || vid == "" {
		return
	}
	c.brokenMu.Lock()
	byVid := c.broken[club]
	if byVid == nil {
		byVid = map[string]map[string]int64{}
		c.broken[club] = byVid
	}
	reps := byVid[vid]
	if reps == nil {
		reps = map[string]int64{}
		byVid[vid] = reps
	}
	reps[ev.PubKey] = int64(ev.CreatedAt) * 1000
	c.brokenMu.Unlock()
}

// brokenSkip reports whether the running track should be skipped as unplayable: an authorized
// reporter (owner/mod or the playing DJ) reported it broken, or ≥ quorum distinct members did.
func (c *conductor) brokenSkip(club string, pb *condClub, now int64) bool {
	if !pb.playing || pb.videoID == "" {
		return false
	}
	c.brokenMu.Lock()
	reps := c.broken[club][pb.videoID]
	fresh := make([]string, 0, len(reps))
	for pk, ts := range reps {
		if now-ts < brokenWindowMS {
			fresh = append(fresh, pk)
		}
	}
	c.brokenMu.Unlock()
	if len(fresh) >= brokenQuorum {
		return true
	}
	for _, pk := range fresh {
		if c.isSkipAuthorized(club, pk, pb.dj) {
			return true
		}
	}
	return false
}

// pruneBroken drops stale broken-track reports so the map stays bounded.
func (c *conductor) pruneBroken(now int64) {
	c.brokenMu.Lock()
	defer c.brokenMu.Unlock()
	for club, byVid := range c.broken {
		for vid, reps := range byVid {
			for pk, ts := range reps {
				if now-ts >= brokenWindowMS {
					delete(reps, pk)
				}
			}
			if len(reps) == 0 {
				delete(byVid, vid)
			}
		}
		if len(byVid) == 0 {
			delete(c.broken, club)
		}
	}
}

// isSkipAuthorized reports whether `pk` may skip the running track of `club`: a club owner or
// moderator, OR the DJ whose track is currently playing (they may skip their own track, e.g. a
// broken video). Roles come from the relay29 in-memory group state (39001/39002 are served
// dynamically, NOT stored — so they can't be queried from the DB). Reads group.Members the same
// lock-free way relay29's own query handlers do; the recover guards the rare concurrent-map race.
func (c *conductor) isSkipAuthorized(club, pk, currentDJ string) (ok bool) {
	if pk == "" {
		return false
	}
	if pk == currentDJ {
		return true
	}
	defer func() {
		if recover() != nil {
			ok = false
		}
	}()
	group, found := c.state.Groups.Load(club)
	if !found || group == nil {
		return false
	}
	for _, role := range group.Members[pk] {
		if role != nil && (role.Name == "owner" || role.Name == "moderator") {
			return true
		}
	}
	return false
}

// observePresence records ephemeral presence beats (kind 20100) so the conductor knows which
// DJs are here RIGHT NOW. Registered on the relay's OnEphemeralEvent (presence isn't stored, so
// it must be captured live, like listeners.observe). Used to decide whether to trust a DJ's
// queue `active` flags (present) or guard against replay with the played-set (away).
func (c *conductor) observePresence(_ context.Context, ev *nostr.Event) {
	if ev.Kind != 20100 {
		return
	}
	club := tagVal(ev, "h")
	if club == "" {
		return
	}
	c.presMu.Lock()
	m := c.pres[club]
	if m == nil {
		m = map[string]int64{}
		c.pres[club] = m
	}
	if t := int64(ev.CreatedAt) * 1000; t > m[ev.PubKey] {
		m[ev.PubKey] = t
	}
	c.presMu.Unlock()
}

// online reports whether a DJ has beaten presence within the online window.
func (c *conductor) online(club, pk string, now int64) bool {
	c.presMu.Lock()
	defer c.presMu.Unlock()
	if m := c.pres[club]; m != nil {
		return now-m[pk] < condOnlineMS
	}
	return false
}

// prunePresence drops stale presence entries so the map stays bounded.
func (c *conductor) prunePresence(now int64) {
	c.presMu.Lock()
	defer c.presMu.Unlock()
	for club, m := range c.pres {
		for pk, t := range m {
			if now-t >= 2*condOnlineMS {
				delete(m, pk)
			}
		}
		if len(m) == 0 {
			delete(c.pres, club)
		}
	}
}

func (c *conductor) run() {
	ticker := time.NewTicker(condTickMS * time.Millisecond)
	defer ticker.Stop()
	for range ticker.C {
		c.tick()
	}
}

func (c *conductor) tick() {
	// A panic in one club's logic must never kill the scheduler for all clubs.
	defer func() {
		if r := recover(); r != nil {
			log.Printf("conductor tick panic: %v", r)
		}
	}()
	ctx := context.Background()
	active := c.activeClubs(ctx)
	// Auto DJ: clubs with a premium owner-armed playlist but no real DJ on stage.
	// armedAutoClubs publishes a 30111 disarm marker when a real DJ takes over.
	auto := c.armedAutoClubs(ctx, active)

	c.mu.Lock()
	defer c.mu.Unlock()
	// Forget state for clubs with no active real DJ AND no auto-DJ.
	for club := range c.clubs {
		if _, ok := active[club]; ok {
			continue
		}
		if _, ok := auto[club]; ok {
			continue
		}
		delete(c.clubs, club)
	}
	now := time.Now().UnixMilli()
	c.prunePresence(now)
	c.pruneBroken(now)
	for club, djs := range active {
		c.driveClub(ctx, club, djs, now)
	}
	for club, st := range auto {
		c.driveAutoClub(ctx, club, st, now)
	}
}

// ── reading club state from the store ────────────────────────────────────────

// activeClubs returns, per club with ≥1 active stage DJ, the DJ list in round-robin order
// (oldest `since` first, pubkey tiebreak, capped) — the same selection as conductor.ts
// selectActiveDjs: on + fresh (<1h) + not kicked.
func (c *conductor) activeClubs(ctx context.Context) map[string][]condDJ {
	type stageEv struct {
		since, lastSeen int64
		on              bool
	}
	stages := map[string]map[string]stageEv{} // club → dj → newest stage event

	ch, err := c.db.QueryEvents(ctx, nostr.Filter{Kinds: []int{kindStage}})
	if err != nil {
		log.Printf("conductor: query stage: %v", err)
		return nil
	}
	for ev := range ch {
		club := tagVal(ev, "h")
		if club == "" {
			continue
		}
		lastSeen := int64(ev.CreatedAt) * 1000
		m := stages[club]
		if m == nil {
			m = map[string]stageEv{}
			stages[club] = m
		}
		if ex, ok := m[ev.PubKey]; ok && lastSeen < ex.lastSeen {
			continue // keep the newest per DJ
		}
		since := int64(ev.CreatedAt)
		if s := tagVal(ev, "since"); s != "" {
			if v, err := strconv.ParseInt(s, 10, 64); err == nil {
				since = v
			}
		}
		m[ev.PubKey] = stageEv{since: since, lastSeen: lastSeen, on: ev.Content != "off"}
	}
	if len(stages) == 0 {
		return nil
	}

	kicks := c.kicksByClub(ctx) // club → dj → newest kick (ms)
	now := time.Now().UnixMilli()
	out := map[string][]condDJ{}
	for club, djmap := range stages {
		var list []condDJ
		for pk, s := range djmap {
			if !s.on || now-s.lastSeen >= condStageStaleMS {
				continue
			}
			if k := kicks[club]; k != nil && s.lastSeen <= k[pk] {
				continue // kicked after their last heartbeat
			}
			list = append(list, condDJ{pubkey: pk, since: s.since})
		}
		if len(list) == 0 {
			continue
		}
		sort.Slice(list, func(i, j int) bool {
			if list[i].since != list[j].since {
				return list[i].since < list[j].since
			}
			return list[i].pubkey < list[j].pubkey
		})
		cap := condMaxDJsFree
		if condPrem != nil {
			if owner := c.clubOwner(ctx, club); owner != "" && condPrem.valid(ctx, owner) {
				cap = condMaxDJs
			}
		}
		if len(list) > cap {
			list = list[:cap]
		}
		out[club] = list
	}
	return out
}

func (c *conductor) kicksByClub(ctx context.Context) map[string]map[string]int64 {
	out := map[string]map[string]int64{}
	ch, err := c.db.QueryEvents(ctx, nostr.Filter{Kinds: []int{kindStageKick}})
	if err != nil {
		return out
	}
	for ev := range ch {
		club := tagVal(ev, "h")
		dj := tagVal(ev, "d")
		if club == "" || dj == "" {
			continue
		}
		m := out[club]
		if m == nil {
			m = map[string]int64{}
			out[club] = m
		}
		if t := int64(ev.CreatedAt) * 1000; t > m[dj] {
			m[dj] = t
		}
	}
	return out
}

// clubQueues returns the newest queue (kind 30103) per DJ for a club, parsed to tracks.
func (c *conductor) clubQueues(ctx context.Context, club string) map[string][]condTrack {
	out := map[string][]condTrack{}
	at := map[string]int64{}
	ch, err := c.db.QueryEvents(ctx, nostr.Filter{Kinds: []int{kindQueue}, Tags: nostr.TagMap{"h": []string{club}}})
	if err != nil {
		return out
	}
	for ev := range ch {
		if t, ok := at[ev.PubKey]; ok && int64(ev.CreatedAt) < t {
			continue
		}
		at[ev.PubKey] = int64(ev.CreatedAt)
		out[ev.PubKey] = parseQueueTracks(ev)
	}
	return out
}

func parseQueueTracks(ev *nostr.Event) []condTrack {
	var out []condTrack
	for _, t := range ev.Tags {
		if len(t) < 2 || t[0] != "track" || !strings.HasPrefix(t[1], "yt:") {
			continue
		}
		title := t[1]
		if len(t) >= 3 {
			title = t[2]
		}
		dur := 0
		if len(t) >= 4 {
			dur, _ = strconv.Atoi(t[3])
		}
		// 5th element 'off' = already played/disabled.
		active := !(len(t) >= 5 && t[4] == "off")
		out = append(out, condTrack{videoID: strings.TrimPrefix(t[1], "yt:"), title: title, duration: dur, active: active})
	}
	return out
}

// ── driving a club ───────────────────────────────────────────────────────────

func (c *conductor) driveClub(ctx context.Context, club string, djs []condDJ, now int64) {
	queues := c.clubQueues(ctx, club)
	pb := c.clubs[club]
	if pb == nil {
		pb = c.resume(ctx, club)
		c.clubs[club] = pb
	}
	djPks := make([]string, len(djs))
	for i, d := range djs {
		djPks[i] = d.pubkey
	}

	// Not playing yet → bootstrap from the round-robin.
	if !pb.playing {
		c.advance(ctx, club, djPks, queues, pb, now)
		return
	}
	// Orphan guard: the playing DJ left the stage → move on.
	if pb.dj != "" && indexOf(djPks, pb.dj) < 0 {
		c.advance(ctx, club, djPks, queues, pb, now)
		return
	}
	// Takeover freeze: a staged DJ is live (kind 30109, mode=takeover, status=live).
	// Freeze the round-robin and publish a paused now_playing so clients stop the YT clock.
	// When the session ends/goes stale, resume with one advance() from the current position.
	if wasTakeover, resuming := c.takeoverState(ctx, club, djPks, pb, now); wasTakeover {
		if resuming {
			// Session just ended → clean resume from current round-robin position.
			c.advance(ctx, club, djPks, queues, pb, now)
		}
		return
	}
	// Skip requested (kind 30107, owner/mod/playing-DJ — validated in skipRequested).
	if c.skipRequested(ctx, club, pb) {
		c.advance(ctx, club, djPks, queues, pb, now)
		return
	}
	// Track reported unplayable (kind 20102) by an authorized user or a quorum of members.
	if c.brokenSkip(club, pb, now) {
		c.advance(ctx, club, djPks, queues, pb, now)
		return
	}
	// Track finished (fallback cap when duration unknown)?
	dur := pb.duration
	if dur <= 0 {
		dur = condMaxTrackFallbackS
	}
	if (now-pb.startedAt)/1000 >= int64(dur) {
		c.advance(ctx, club, djPks, queues, pb, now)
		return
	}
	// Otherwise: heartbeat on cadence (republish the frozen track — pos is a stable token).
	if now-pb.lastBeat >= condHeartbeatMS {
		c.publishNowPlaying(ctx, club, pb, now)
	}
}

// advance picks the next playable track (fair round-robin) and starts it. The DJs take turns
// starting after the one who just played; each offers its TOP ACTIVE (not-`off`) track — its
// position 1 — so the interleave alternates fairly per DJ and a drag-and-drop reorder takes
// effect immediately. The visible queue is the only truth — a played track is `off` (client-
// marked) and drops out; truly nothing playable → lobby (stop). No hidden played-set. `pos` is
// an opaque monotonic token per started track (skip-request matching + now_playing/play-log).
func (c *conductor) advance(ctx context.Context, club string, djPks []string, queues map[string][]condTrack, pb *condClub, now int64) {
	if len(djPks) == 0 {
		c.stop(pb)
		return
	}
	di, ti := fairNext(djPks, pb.dj, c.matrix(djPks, queues, pb))
	if di == -1 {
		// Every DJ's queue is fully played-off (or empty) → stop to the lobby. No auto-loop: a set
		// plays through once, then the lobby runs until a DJ adds / re-activates tracks.
		c.stop(pb)
		return
	}
	tracks := queues[djPks[di]]
	if ti >= len(tracks) {
		c.stop(pb)
		return
	}
	t := tracks[ti]
	pb.pos++
	pb.dj = djPks[di]
	pb.videoID = t.videoID
	pb.title = t.title
	pb.duration = t.duration
	pb.startedAt = now
	pb.playing = true
	c.publishNowPlaying(ctx, club, pb, now)
	c.publishPlay(ctx, club, pb, now)
}

// matrix builds the playability matrix. A track is playable if it is `active` (NOT marked `off`)
// and not the currently-playing one. The DJ's QUEUE is the single source of truth: a played track
// becomes `off` (client-marked) and drops out; reordering changes which active track is on top; a
// manually re-activated track (`off`→active) plays again. The relay does NOT keep its own hidden
// played-set — that would override the visible queue (e.g. exclude a re-activated track the DJ
// put back at the top). Mirrors the client's playableMatrix (sync.svelte.ts) exactly.
func (c *conductor) matrix(djPks []string, queues map[string][]condTrack, pb *condClub) [][]bool {
	out := make([][]bool, len(djPks))
	for i, pk := range djPks {
		tracks := queues[pk]
		row := make([]bool, len(tracks))
		for j, t := range tracks {
			row[j] = t.active && t.videoID != pb.videoID
		}
		out[i] = row
	}
	return out
}

// skipRequested reports whether a fresh, AUTHORIZED skip-request (kind 30107) targets the
// running track. The request must (a) name the current pos, (b) be newer than the current
// track's start (so a stale request from a previous track is ignored, and the relay never
// re-acts after advancing — the new track has a different pos/start), and (c) come from a club
// owner/moderator or the currently-playing DJ. The relay accepts any member's 30107 into the
// store (it knows no kind allowlist), so the role check is enforced HERE.
func (c *conductor) skipRequested(ctx context.Context, club string, pb *condClub) bool {
	if !pb.playing {
		return false
	}
	ch, err := c.db.QueryEvents(ctx, nostr.Filter{Kinds: []int{kindSkip}, Tags: nostr.TagMap{"h": []string{club}}})
	if err != nil {
		return false
	}
	var latest *nostr.Event
	for ev := range ch {
		if latest == nil || ev.CreatedAt > latest.CreatedAt {
			latest = ev
		}
	}
	if latest == nil {
		return false
	}
	if atoiDefault(tagVal(latest, "pos"), -1) != pb.pos {
		return false
	}
	if int64(latest.CreatedAt)*1000 < pb.startedAt {
		return false
	}
	return c.isSkipAuthorized(club, latest.PubKey, pb.dj)
}

func (c *conductor) stop(pb *condClub) {
	// Stop beating → clients see now_playing go stale and fall back to the lobby track. (Deletion
	// of the lingering replaceable isn't needed.)
	//
	// KEEP pb.videoID/dj — do NOT clear them. The next tick re-runs advance() (bootstrap), and the
	// matrix excludes pb.videoID. If we cleared it, the just-played final track would no longer be
	// excluded; when the DJ's client hasn't marked it `off` yet (e.g. they closed the tab as it
	// ended), the bootstrap would re-pick it and the LAST SONG WOULD LOOP. Retaining it holds that
	// one track back until a different/new active track plays (which changes pb.videoID) — so a set
	// plays through once, then the lobby runs. No auto-loop.
	pb.playing = false
}

// ── publishing (relay-signed, straight to the store, bypassing RejectEvent) ───

func (c *conductor) publishNowPlaying(ctx context.Context, club string, pb *condClub, now int64) {
	ev := &nostr.Event{
		Kind:      kindNowPlaying,
		CreatedAt: nostr.Timestamp(now / 1000),
		Tags: nostr.Tags{
			{"h", club},
			{"d", club},
			{"track", "yt:" + pb.videoID},
			{"dj", pb.dj},
			{"pos", strconv.Itoa(pb.pos)},
			{"started_at", strconv.FormatInt(pb.startedAt, 10)},
			{"sent_at", strconv.FormatInt(now, 10)},
			{"duration", strconv.Itoa(pb.duration)},
			{"status", "playing"},
		},
		Content: pb.title,
	}
	pb.lastBeat = now
	c.publish(ctx, ev, true) // 30100 is addressable → replace
}

func (c *conductor) publishPlay(ctx context.Context, club string, pb *condClub, now int64) {
	ev := &nostr.Event{
		Kind:      kindPlay,
		CreatedAt: nostr.Timestamp(now / 1000),
		Tags: nostr.Tags{
			{"h", club},
			{"p", pb.dj},
			{"started_at", strconv.FormatInt(pb.startedAt, 10)},
			{"pos", strconv.Itoa(pb.pos)},
		},
		Content: pb.videoID,
	}
	c.publish(ctx, ev, false) // 1313 is a regular (non-replaceable) record
}

// publish signs an event with the relay key and writes it straight to the store (+ broadcast),
// bypassing the RejectEvent chain — the relay's own events are trusted.
func (c *conductor) publish(ctx context.Context, ev *nostr.Event, replace bool) {
	if err := ev.Sign(c.sk); err != nil {
		log.Printf("conductor sign kind %d: %v", ev.Kind, err)
		return
	}
	var err error
	if replace {
		err = c.db.ReplaceEvent(ctx, ev)
	} else {
		err = c.db.SaveEvent(ctx, ev)
	}
	if err != nil {
		log.Printf("conductor store kind %d: %v", ev.Kind, err)
		return
	}
	c.relay.BroadcastEvent(ev)
}

// ── cold-start resume ─────────────────────────────────────────────────────────

// resume rebuilds a club's playback state after a relay restart from the newest relay-authored
// now_playing (continue the current track) + recent 1313 plays (re-seed the played-set/loop).
// Best-effort: if no relay now_playing exists, returns an empty state (the next tick bootstraps).
func (c *conductor) resume(ctx context.Context, club string) *condClub {
	pb := &condClub{}
	ch, err := c.db.QueryEvents(ctx, nostr.Filter{Kinds: []int{kindNowPlaying}, Tags: nostr.TagMap{"h": []string{club}}})
	if err != nil {
		return pb
	}
	var latest *nostr.Event
	for ev := range ch {
		if latest == nil || ev.CreatedAt > latest.CreatedAt {
			latest = ev
		}
	}
	if latest == nil || latest.PubKey != c.pub {
		return pb // no prior relay-driven track → fresh bootstrap
	}
	track := tagVal(latest, "track")
	pb.videoID = strings.TrimPrefix(track, "yt:")
	pb.dj = tagVal(latest, "dj")
	pb.title = latest.Content
	pb.pos = atoiDefault(tagVal(latest, "pos"), 0)
	pb.duration = atoiDefault(tagVal(latest, "duration"), 0)
	if v, err := strconv.ParseInt(tagVal(latest, "started_at"), 10, 64); err == nil {
		pb.startedAt = v
	}
	pb.playing = pb.videoID != ""
	return pb
}

// ── helpers ───────────────────────────────────────────────────────────────────
// (tagVal lives in entryfee.go)

func atoiDefault(s string, def int) int {
	if s == "" {
		return def
	}
	if n, err := strconv.Atoi(s); err == nil {
		return n
	}
	return def
}

// ── takeover (live A/V session) ───────────────────────────────────────────────

const liveSessionStaleMS = 300_000 // 5 min; a live-session heartbeat must arrive within this

// takeoverState checks whether a kind-30109 takeover live-session is active for the club.
// Returns (wasTakeover bool, resuming bool):
//   - wasTakeover=true, resuming=false  → session is still live; freeze the round-robin.
//   - wasTakeover=true, resuming=true   → session just ended; caller should call advance().
//   - wasTakeover=false, resuming=false → no live session; normal flow.
//
// Side-effects: on the first tick of a live takeover, publishes now_playing with
// status=paused so clients immediately stop the YT clock.
func (c *conductor) takeoverState(ctx context.Context, club string, djPks []string, pb *condClub, now int64) (wasTakeover, resuming bool) {
	// Check for a fresh, live takeover event from any current staged DJ.
	active := c.activeTakeover(ctx, club, djPks, now)
	if active {
		if !pb.inTakeover {
			// Transition into takeover: publish paused now_playing once.
			pb.inTakeover = true
			c.publishNowPlayingPaused(ctx, club, pb, now)
		} else if now-pb.lastBeat >= condHeartbeatMS {
			// Already in takeover: keep heartbeating so late joiners get the paused state.
			c.publishNowPlayingPaused(ctx, club, pb, now)
		}
		return true, false
	}
	if pb.inTakeover {
		// Session just ended → signal the caller to resume.
		pb.inTakeover = false
		return true, true
	}
	return false, false
}

// activeTakeover returns true when there is a fresh kind-30109 takeover event authored by
// an active staged DJ with status=live.
func (c *conductor) activeTakeover(ctx context.Context, club string, djPks []string, now int64) bool {
	ch, err := c.db.QueryEvents(ctx, nostr.Filter{
		Kinds: []int{kindLiveSession},
		Tags:  nostr.TagMap{"h": []string{club}},
	})
	if err != nil {
		return false
	}
	// Collect newest per author.
	newest := map[string]*nostr.Event{}
	for ev := range ch {
		existing, ok := newest[ev.PubKey]
		if !ok || ev.CreatedAt > existing.CreatedAt {
			newest[ev.PubKey] = ev
		}
	}
	stale := now - liveSessionStaleMS
	for _, ev := range newest {
		if tagVal(ev, "mode") != "takeover" {
			continue
		}
		if tagVal(ev, "status") != "live" {
			continue
		}
		if int64(ev.CreatedAt)*1000 < stale {
			continue
		}
		// Author must be an active staged DJ (still on stage, not just any member).
		if indexOf(djPks, ev.PubKey) >= 0 {
			return true
		}
	}
	return false
}

// publishNowPlayingPaused republishes now_playing with status=paused so clients stop the
// YT clock. pos/startedAt are frozen so the position is preserved for when we resume.
func (c *conductor) publishNowPlayingPaused(ctx context.Context, club string, pb *condClub, now int64) {
	ev := &nostr.Event{
		Kind:      kindNowPlaying,
		CreatedAt: nostr.Timestamp(now / 1000),
		Tags: nostr.Tags{
			{"h", club},
			{"d", club},
			{"track", "yt:" + pb.videoID},
			{"dj", pb.dj},
			{"pos", strconv.Itoa(pb.pos)},
			{"started_at", strconv.FormatInt(pb.startedAt, 10)},
			{"sent_at", strconv.FormatInt(now, 10)},
			{"duration", strconv.Itoa(pb.duration)},
			{"status", "paused"},
		},
		Content: pb.title,
	}
	pb.lastBeat = now
	c.publish(ctx, ev, true)
}

// ── Auto DJ ───────────────────────────────────────────────────────────────────

// makeShuffledOrder returns a slice [0, n) in a random order.
func makeShuffledOrder(n int) []int {
	order := make([]int, n)
	for i := range order {
		order[i] = i
	}
	rand.Shuffle(n, func(i, j int) { order[i], order[j] = order[j], order[i] })
	return order
}

// armedAutoClubs returns clubs with an owner-armed Auto DJ playlist and no real DJ on stage.
// When a real DJ takes over an armed club, it publishes a 30111 disarm marker (once, idempotent).
func (c *conductor) armedAutoClubs(ctx context.Context, active map[string][]condDJ) map[string]*autoState {
	type armedEntry struct {
		ev    *nostr.Event
		owner string
	}

	ch, err := c.db.QueryEvents(ctx, nostr.Filter{Kinds: []int{kindAutoDJ}})
	if err != nil {
		return nil
	}
	armed := map[string]armedEntry{} // club → newest armed 30105
	for ev := range ch {
		club := tagVal(ev, "h")
		if club == "" || tagVal(ev, "status") != "armed" {
			continue
		}
		if ex, ok := armed[club]; !ok || ev.CreatedAt > ex.ev.CreatedAt {
			armed[club] = armedEntry{ev: ev, owner: ev.PubKey}
		}
	}
	if len(armed) == 0 {
		return nil
	}

	// Query relay-signed disarm markers (kind 30111, replaceable per d=club).
	ch2, err := c.db.QueryEvents(ctx, nostr.Filter{Kinds: []int{kindAutoDJCtrl}})
	if err != nil {
		return nil
	}
	disarmedAt := map[string]nostr.Timestamp{} // club → newest 30111 created_at
	for ev := range ch2 {
		club := tagVal(ev, "d")
		if club == "" || ev.PubKey != c.pub {
			continue // only trust relay-signed disarm markers
		}
		if t, ok := disarmedAt[club]; !ok || ev.CreatedAt > t {
			disarmedAt[club] = ev.CreatedAt
		}
	}

	out := map[string]*autoState{}
	for club, entry := range armed {
		// Effective-armed: no 30111 disarm marker with created_at >= the armed event's created_at.
		disarmFresh := disarmedAt[club] >= entry.ev.CreatedAt

		if _, hasRealDJ := active[club]; hasRealDJ {
			// A real DJ is on stage → publish a disarm marker once (idempotent).
			if !disarmFresh {
				c.publishAutoDJDisarm(ctx, club)
			}
			continue // real DJ drives this club via driveClub
		}

		if disarmFresh {
			continue // disarmed; owner must manually re-arm
		}

		tracks := parseQueueTracks(entry.ev)
		if len(tracks) == 0 {
			continue
		}
		out[club] = &autoState{owner: entry.owner, tracks: tracks}
	}
	return out
}

// driveAutoClub drives a club in Auto DJ mode: shuffled rotation of an owner-armed playlist,
// with no real DJ on stage. Completely separate from the real-DJ round-robin.
func (c *conductor) driveAutoClub(ctx context.Context, club string, st *autoState, now int64) {
	pb := c.clubs[club]
	if pb == nil {
		pb = c.resume(ctx, club)
		c.clubs[club] = pb
	}

	tracks := st.tracks
	if len(tracks) == 0 {
		pb.playing = false
		return
	}

	// (Re)initialize the shuffle order when the playlist changes or on cold start.
	if len(pb.autoOrder) != len(tracks) {
		pb.autoOrder = makeShuffledOrder(len(tracks))
		pb.autoIdx = 0
	}

	nextTrack := func() {
		pb.autoIdx++
		if pb.autoIdx >= len(tracks) {
			pb.autoOrder = makeShuffledOrder(len(tracks))
			pb.autoIdx = 0
		}
	}

	startTrack := func() {
		t := tracks[pb.autoOrder[pb.autoIdx]]
		pb.pos++
		pb.dj = st.owner
		pb.videoID = t.videoID
		pb.title = t.title
		pb.duration = t.duration
		pb.startedAt = now
		pb.playing = true
		c.publishNowPlayingAuto(ctx, club, pb, st.owner, now)
		c.publishPlay(ctx, club, pb, now)
	}

	if !pb.playing {
		startTrack()
		return
	}

	if c.skipRequested(ctx, club, pb) || c.brokenSkip(club, pb, now) {
		nextTrack()
		startTrack()
		return
	}

	dur := pb.duration
	if dur <= 0 {
		dur = condMaxTrackFallbackS
	}
	if (now-pb.startedAt)/1000 >= int64(dur) {
		nextTrack()
		startTrack()
		return
	}

	if now-pb.lastBeat >= condHeartbeatMS {
		c.publishNowPlayingAuto(ctx, club, pb, st.owner, now)
	}
}

// publishNowPlayingAuto emits now_playing with p=owner (zap target) and auto=1 tags,
// used when Auto DJ drives the club with no real DJ on stage.
func (c *conductor) publishNowPlayingAuto(ctx context.Context, club string, pb *condClub, owner string, now int64) {
	ev := &nostr.Event{
		Kind:      kindNowPlaying,
		CreatedAt: nostr.Timestamp(now / 1000),
		Tags: nostr.Tags{
			{"h", club},
			{"d", club},
			{"track", "yt:" + pb.videoID},
			{"dj", pb.dj},
			{"pos", strconv.Itoa(pb.pos)},
			{"started_at", strconv.FormatInt(pb.startedAt, 10)},
			{"sent_at", strconv.FormatInt(now, 10)},
			{"duration", strconv.Itoa(pb.duration)},
			{"status", "playing"},
			{"p", owner},
			{"auto", "1"},
		},
		Content: pb.title,
	}
	pb.lastBeat = now
	c.publish(ctx, ev, true)
}

// publishAutoDJDisarm emits a relay-signed kind-30111 disarm marker when a real DJ takes
// over an armed-auto club. Replaceable per club (d=club).
func (c *conductor) publishAutoDJDisarm(ctx context.Context, club string) {
	ev := &nostr.Event{
		Kind:      kindAutoDJCtrl,
		CreatedAt: nostr.Timestamp(time.Now().Unix()),
		Tags: nostr.Tags{
			{"h", club},
			{"d", club},
			{"armed", "0"},
		},
		Content: "",
	}
	c.publish(ctx, ev, true)
}

// ── stageGate: enforce per-club DJ slot cap on kind-30102 events ─────────────

type stageGate struct {
	db         *badger.BadgerBackend
	prem       *premiumStore
	superadmin string
}

// reject blocks a stage-join (kind 30102, content != "off") if the club is
// already at its DJ cap (2 for free owners, 5 for premium owners).
// Heartbeats from DJs already on stage are always allowed through.
func (g *stageGate) reject(ctx context.Context, evt *nostr.Event) (bool, string) {
	if evt.Kind != kindStage {
		return false, ""
	}
	if evt.Content == "off" {
		return false, "" // leaving stage is always allowed
	}
	club := tagVal(evt, "h")
	if club == "" {
		return false, ""
	}
	if g.superadmin != "" && evt.PubKey == g.superadmin {
		return false, ""
	}

	// Count active DJs on stage (excluding the sender — heartbeat is allowed).
	ch, err := g.db.QueryEvents(ctx, nostr.Filter{
		Kinds: []int{kindStage},
		Tags:  nostr.TagMap{"h": []string{club}},
	})
	if err != nil {
		return false, ""
	}
	staleThreshold := time.Now().UnixMilli() - condStageStaleMS
	active := 0
	alreadyOnStage := false
	for ev := range ch {
		if ev.PubKey == evt.PubKey {
			alreadyOnStage = true
			continue
		}
		if ev.Content == "off" {
			continue
		}
		if int64(ev.CreatedAt)*1000 >= staleThreshold {
			active++
		}
	}
	if alreadyOnStage {
		return false, "" // heartbeat from existing DJ
	}

	cap := condMaxDJsFree
	if g.prem != nil {
		if owner := clubOwnerFromDB(ctx, g.db, club); owner != "" && g.prem.valid(ctx, owner) {
			cap = condMaxDJs
		}
	}
	if active >= cap {
		return true, "restricted: stage is full"
	}
	return false, ""
}

func clubOwnerFromDB(ctx context.Context, db *badger.BadgerBackend, club string) string {
	ch, err := db.QueryEvents(ctx, nostr.Filter{
		Kinds: []int{kindCreateGroup},
		Tags:  nostr.TagMap{"h": []string{club}},
	})
	if err != nil {
		return ""
	}
	var newest *nostr.Event
	for ev := range ch {
		if newest == nil || ev.CreatedAt > newest.CreatedAt {
			newest = ev
		}
	}
	if newest == nil {
		return ""
	}
	return newest.PubKey
}
