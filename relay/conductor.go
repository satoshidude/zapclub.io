package main

import (
	"context"
	"log"
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

const (
	condMaxDJs            = 5            // UI/slot cap (matches conductor.ts MAX_DJS)
	condStageStaleMS      = 3_600_000    // sticky stage: 1h after last heartbeat (STALE_MS)
	condOnlineMS          = 50_000       // a DJ is "present" within this of their last 20100 beat
	condHeartbeatMS       = 15_000       // now_playing republish cadence (latecomers + drift)
	condMaxTrackFallbackS = 600          // duration<=0 → cap so a missing-duration track ends
	condTickMS            = 2500         // scheduler granularity (precise enough track-end)
	condSessionLookbackMS = 150_000      // resume: plays within this window = the live session
	kindNowPlaying        = 30100
	kindStage             = 30102
	kindQueue             = 30103
	kindStageKick         = 30106
	kindSkip              = 30107
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
	pos       int
	loop      int
	played    map[string]bool // videoIds played this rotation
	videoID   string
	dj        string
	title     string
	duration  int
	startedAt int64 // ms (relay clock)
	lastBeat  int64 // ms of the last now_playing publish
	playing   bool
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

	c.mu.Lock()
	defer c.mu.Unlock()
	// Forget state for clubs that no longer have any active DJ (→ lobby).
	for club := range c.clubs {
		if _, ok := active[club]; !ok {
			delete(c.clubs, club)
		}
	}
	now := time.Now().UnixMilli()
	c.prunePresence(now)
	c.pruneBroken(now)
	for club, djs := range active {
		c.driveClub(ctx, club, djs, now)
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
		if len(list) > condMaxDJs {
			list = list[:condMaxDJs]
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
	// Otherwise: heartbeat on cadence (republish frozen track, pos re-anchored).
	if now-pb.lastBeat >= condHeartbeatMS {
		pb.pos = reanchoredPos(djPks, pb.dj, pb.pos, pb.videoID, queueIDsFn(queues))
		c.publishNowPlaying(ctx, club, pb, now)
	}
}

// advance picks the next playable track (scan-from-top, round-robin) and starts it. Excludes
// the current track and anything played this rotation; on exhaustion it bumps the loop epoch
// and clears the played-set so the rotation repeats; truly nothing playable → lobby (stop).
func (c *conductor) advance(ctx context.Context, club string, djPks []string, queues map[string][]condTrack, pb *condClub, now int64) {
	if len(djPks) == 0 {
		c.stop(pb)
		return
	}
	if pb.played == nil {
		pb.played = map[string]bool{}
	}
	if pb.videoID != "" {
		pb.played[pb.videoID] = true
	}
	// Always scan from the TOP of each DJ's queue (round-robin over each DJ's first PLAYABLE
	// track): the next track is position 1 of the playlist, so a DJ's drag-and-drop reorder takes
	// effect immediately on the next advance. The played-set (excluded for ALL DJs in matrix())
	// makes this safe — already-played tracks drop out, so "first playable" walks forward and can
	// never oscillate (the old oscillation came from NOT excluding a present DJ's played tracks).
	next := firstPlayablePos(len(djPks), c.matrix(club, djPks, queues, pb, now))
	if next == -1 {
		// Every DJ's queue is fully played (or disabled) → stop to the lobby. NO auto-loop: a set
		// plays through exactly once, then the lobby placeholder runs until a DJ adds tracks (or
		// the next DJ has a non-empty, not-yet-played queue). Replaying a track = the DJ re-adds it.
		c.stop(pb)
		return
	}
	di, ti := posToSlot(next, len(djPks))
	tracks := queues[djPks[di]]
	if ti >= len(tracks) {
		c.stop(pb)
		return
	}
	t := tracks[ti]
	pb.pos = next
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
func (c *conductor) matrix(club string, djPks []string, queues map[string][]condTrack, pb *condClub, now int64) [][]bool {
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
	// Stop beating → clients see now_playing go stale and fall back to the lobby track. (Matches
	// the old client behaviour; deletion of the lingering replaceable isn't needed.)
	pb.playing = false
	pb.videoID = ""
	pb.dj = ""
}

func queueIDsFn(queues map[string][]condTrack) func(string) []string {
	return func(dj string) []string {
		tracks := queues[dj]
		ids := make([]string, len(tracks))
		for i, t := range tracks {
			ids[i] = t.videoID
		}
		return ids
	}
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
			{"loop", strconv.Itoa(pb.loop)},
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
	pb := &condClub{played: map[string]bool{}}
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
	c.seedPlayed(ctx, club, pb)
	return pb
}

// seedPlayed marks recent (this-session) plays as played and adopts the highest loop epoch, so a
// post-restart rotation continues instead of replaying from the top. Bounded window keeps it cheap.
func (c *conductor) seedPlayed(ctx context.Context, club string, pb *condClub) {
	since := nostr.Timestamp((time.Now().UnixMilli() - condSessionLookbackMS) / 1000)
	ch, err := c.db.QueryEvents(ctx, nostr.Filter{Kinds: []int{kindPlay}, Tags: nostr.TagMap{"h": []string{club}}, Since: &since})
	if err != nil {
		return
	}
	for ev := range ch {
		if vid := ev.Content; vid != "" && vid != pb.videoID {
			pb.played[vid] = true
		}
		if l := atoiDefault(tagVal(ev, "loop"), 0); l > pb.loop {
			pb.loop = l
		}
	}
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
