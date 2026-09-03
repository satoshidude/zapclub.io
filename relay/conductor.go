package main

import (
	"context"
	"database/sql"
	"fmt"
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

const (
	condMaxDJs            = 5       // maximum DJs per club
	condStageStaleMS      = 300_000 // sticky stage: max 5 min after last heartbeat (STALE_MS; client beats every 2 min)
	condOnlineMS          = 50_000  // a DJ is "present" within this of their last 20100 beat
	condHeartbeatMS       = 15_000  // now_playing republish cadence (latecomers + drift)
	condExhaustedBackoff  = 60_000  // retry delay when all DJ queues are empty/played-through
	condMaxTrackFallbackS = 600     // duration<=0 → cap so a missing-duration track ends
	condTickMS            = 2500    // scheduler granularity (precise enough track-end)
	kindNowPlaying        = 30100
	kindStage             = 30102
	kindQueue             = 30103
	kindStageKick         = 30106
	kindSkip              = 30107
	kindAutoDJ            = 30105 // owner-authored: arm/disarm auto-dj playlist for a club
	kindAutoDJCtrl        = 30111 // relay-signed: disarm marker (real DJ took over)
	kindBroken            = 20102 // ephemeral "I can't play this track" report (content = videoId)
	kindMood              = 20104 // ephemeral vibe vote: tags h=club, pos=track-pos, v=banger|skip
	kindPlay              = 1313
	brokenWindowMS        = 120_000    // a broken-track report counts as fresh this long
	brokenQuorum          = 2          // distinct members reporting a track broken → skip it
	brokenVidBlockMS      = 21_600_000 // a broken-SKIPPED video stays out of the rotation this long (6 h)
	moodSkipThreshold     = 3          // distinct "skip" votes → advance to next track
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
	videoID   string
	dj        string
	title     string
	duration  int
	startedAt int64 // ms (relay clock)
	lastBeat  int64 // ms of the last now_playing publish
	playing   bool
	// Auto DJ state (zero-value = not initialized; reinit on playlist-length change).
	autoOrder []int
	autoIdx   int
}

// autoState carries the owner + parsed tracks for a club in Auto DJ mode.
type autoState struct {
	owner  string
	tracks []condTrack
	since  int64 // ms — from 30105 CreatedAt; used as the DJ's round-robin sort key
}

// stageEntry is the newest known state for one DJ in one club (from 30102 index).
type stageEntry struct {
	since    int64
	lastSeen int64
	on       bool
}

type conductor struct {
	db    *badger.BadgerBackend
	sq    *sql.DB // SQLite writer; nil = disabled (graceful degradation)
	sqr   *sql.DB // SQLite reader (WAL concurrent reads); nil when sq is nil
	relay *khatru.Relay
	state *relay29.State // group membership/roles (skip authorization)
	sk    string
	pub   string
	mu    sync.Mutex
	clubs map[string]*condClub
	// played tracks per (club, dj, videoID) → ms timestamp when played; guarded by mu.
	// Used to block stale-tab republishes: a track is blocked only when the DJ's current
	// 30103 queue version predates the play timestamp (qAddedMs <= pAt). A legitimate
	// re-add writes a newer 30103 (qAddedMs > pAt) and is not blocked.
	played map[string]map[string]map[string]int64

	presMu sync.Mutex
	pres   map[string]map[string]int64 // club → pubkey → last presence beat (ms)

	brokenMu sync.Mutex
	broken   map[string]map[string]map[string]int64 // club → videoId → reporter → ts (ms)

	moodMu     sync.Mutex
	moods      map[string]map[int]map[string]string // club → pos → pubkey → "banger" (bangers only)
	skipCounts map[string]map[int]map[string]int    // club → pos → pubkey → skip count (≤ moodSkipThreshold)

	// Per-club throttle timestamps; guarded by mu.
	// Stored at conductor level (not inside condClub) so they survive condClub being
	// deleted and recreated by the tick cleanup loop.
	bootstrapAt  map[string]int64            // club → last bootstrap attempt ms
	brokenSkipAt map[string]int64            // club → last broken-skip ms
	brokenVids   map[string]map[string]int64 // club → videoID → broken-skip ts (ms); matrix blocks these

	qLogMu     sync.Mutex
	qLogged    map[string]string // club:dj → fingerprint of the last logged queue state (change-only logging)
	moodSkipAt map[string]int64  // club → last mood-skip ms

	// queueWakeup is set by idxQueue (OnEventSaved goroutine) when a DJ-authored 30103 with
	// active tracks arrives. driveClub reads and clears it under c.mu to reset the exhausted
	// backoff so the bootstrap runs immediately instead of waiting condExhaustedBackoff.
	// sync.Map is used to avoid adding c.mu to the OnEventSaved hot path.
	queueWakeup sync.Map // club → struct{}{}

	// Event-driven in-memory indexes (populated by warmIndexes + observeEvent). Replacing
	// per-tick full-table DB scans with O(1) lookups. Guarded by idxMu.
	idxMu         sync.Mutex
	stageIdx      map[string]map[string]stageEntry   // club → pubkey → newest 30102
	kickIdx       map[string]map[string]int64        // club → dj → newest kick ms
	queueIdx      map[string]map[string]*nostr.Event // club → pubkey → newest 30103
	skipIdx       map[string]*nostr.Event            // club → newest 30107
	autoDJIdx     map[string]*nostr.Event            // club → newest 30105
	autoDJCtrlIdx map[string]nostr.Timestamp         // club → newest relay-signed 30111
	ownerCache    map[string]string                  // club → creator pubkey (permanent)

}

func newConductor(db *badger.BadgerBackend, relay *khatru.Relay, state *relay29.State, sk string) *conductor {
	pub, _ := nostr.GetPublicKey(sk)
	return &conductor{
		db: db, relay: relay, state: state, sk: sk, pub: pub,
		clubs:         map[string]*condClub{},
		played:        map[string]map[string]map[string]int64{},
		pres:          map[string]map[string]int64{},
		broken:        map[string]map[string]map[string]int64{},
		moods:         map[string]map[int]map[string]string{},
		skipCounts:    map[string]map[int]map[string]int{},
		bootstrapAt:   map[string]int64{},
		brokenSkipAt:  map[string]int64{},
		brokenVids:    map[string]map[string]int64{},
		qLogged:       map[string]string{},
		moodSkipAt:    map[string]int64{},
		stageIdx:      map[string]map[string]stageEntry{},
		kickIdx:       map[string]map[string]int64{},
		queueIdx:      map[string]map[string]*nostr.Event{},
		skipIdx:       map[string]*nostr.Event{},
		autoDJIdx:     map[string]*nostr.Event{},
		autoDJCtrlIdx: map[string]nostr.Timestamp{},
		ownerCache:    map[string]string{},
	}
}

// ── event-driven indexes ──────────────────────────────────────────────────────

// warmIndexes seeds the in-memory indexes from BadgerDB in parallel at startup.
// After this, observeEvent keeps them current via OnEventSaved.
// Staleable kinds (stage, kick) use a 2-hour since-filter to skip irrelevant old events.
// Replaceable kinds (queue, autoDJ, autoDJCtrl) must NOT be since-filtered — their
// single live record may be arbitrarily old.
func (c *conductor) warmIndexes(ctx context.Context) {
	type kindFilter struct {
		kind  int
		since nostr.Timestamp // zero = no filter
	}
	staleWindow := nostr.Timestamp(time.Now().Unix() - 7200) // 2 h
	kinds := []kindFilter{
		{kindStage, staleWindow},     // 30102: heartbeat, stale after ~1 h
		{kindStageKick, staleWindow}, // 30106: kick marker, similarly time-bound
		{kindQueue, 0},               // 30103: replaceable per DJ/club — keep all
		{kindSkip, 0},                // 30107: replaceable per club — keep latest
		{kindAutoDJ, 0},              // 30105: replaceable per club
		{kindAutoDJCtrl, 0},          // 30111: relay-signed replaceable
	}
	var wg sync.WaitGroup
	for _, kf := range kinds {
		wg.Add(1)
		go func(kf kindFilter) {
			defer wg.Done()
			f := nostr.Filter{Kinds: []int{kf.kind}}
			if kf.since != 0 {
				f.Since = &kf.since
			}
			ch, err := c.db.QueryEvents(ctx, f)
			if err != nil {
				log.Printf("conductor: warm index kind %d: %v", kf.kind, err)
				return
			}
			for ev := range ch {
				c.indexEvent(ev)
			}
		}(kf)
	}
	wg.Wait()
	log.Printf("conductor: indexes warmed")
	c.repairAdmins(ctx)
}

// repairAdmins ensures the stored owner has the "owner" role in relay29's in-memory group state.
// relay29 generates 39001 events on-the-fly from its in-memory Groups map — there is no stored
// 39001 in BadgerDB to query. Instead we: (a) check the live 39001 via AdminsQueryHandler,
// (b) if the owner is missing, publish a kind 9000 put-user event to BadgerDB (persists across
// restarts via loadGroupsFromDB moderation replay) and call state.ApplyModerationAction to update
// the in-memory state and broadcast the corrected 39001 to all connected clients.
func (c *conductor) repairAdmins(ctx context.Context) {
	if c.sqr == nil {
		return
	}
	rows, err := c.sqr.Query(`SELECT club, owner FROM club_owners`)
	if err != nil {
		log.Printf("conductor repairAdmins: sql query: %v", err)
		return
	}
	owners := map[string]string{}
	for rows.Next() {
		var club, owner string
		if err := rows.Scan(&club, &owner); err != nil {
			log.Printf("conductor repairAdmins: scan: %v", err)
			continue
		}
		owners[club] = owner
	}
	if err := rows.Err(); err != nil {
		log.Printf("conductor repairAdmins: rows error: %v", err)
	}
	rows.Close()
	log.Printf("conductor repairAdmins: %d clubs in club_owners", len(owners))
	if len(owners) == 0 {
		return
	}
	for club, owner := range owners {
		c.repairClubAdmins(ctx, club, owner)
	}
}

func (c *conductor) repairClubAdmins(ctx context.Context, club, owner string) {
	// Query relay29's live in-memory 39001 for this club.
	ch, err := c.state.AdminsQueryHandler(ctx, nostr.Filter{
		Kinds: []int{nostr.KindSimpleGroupAdmins},
		Tags:  nostr.TagMap{"d": []string{club}},
	})
	if err != nil {
		log.Printf("conductor repairAdmins [%.8s]: AdminsQueryHandler: %v", club, err)
		return
	}
	var latest *nostr.Event
	for ev := range ch {
		latest = ev
	}
	if latest == nil {
		log.Printf("conductor repairAdmins [%.8s]: no in-memory 39001 yet", club)
		return
	}
	// Check if the owner already has the "owner" role.
	for _, t := range latest.Tags {
		if len(t) >= 2 && t[0] == "p" && t[1] == owner {
			for _, field := range t[2:] {
				if field == "owner" {
					return // already correct
				}
			}
		}
	}

	// Owner missing the "owner" role — publish a kind 9000 put-user event.
	// SaveEvent → persists across restarts (loadGroupsFromDB replays ModerationEventKinds).
	// ApplyModerationAction → updates in-memory state + broadcasts corrected 39001 to clients.
	ev := &nostr.Event{
		Kind:      nostr.KindSimpleGroupPutUser,
		CreatedAt: nostr.Timestamp(time.Now().Unix()),
		Tags: nostr.Tags{
			{"h", club},
			{"p", owner, "", "owner"},
		},
	}
	if err := ev.Sign(c.sk); err != nil {
		log.Printf("conductor repairAdmins [%.8s]: sign: %v", club, err)
		return
	}
	if err := c.db.SaveEvent(ctx, ev); err != nil {
		log.Printf("conductor repairAdmins [%.8s]: save: %v", club, err)
		return
	}
	c.state.ApplyModerationAction(ctx, ev)
	log.Printf("conductor repairAdmins [%.8s]: owner=%.8s fixed in 39001 via kind 9000", club, owner)
}

// observeEvent is registered on relay.OnEventSaved and keeps the indexes current.
func (c *conductor) observeEvent(_ context.Context, ev *nostr.Event) {
	c.indexEvent(ev)
}

func (c *conductor) indexEvent(ev *nostr.Event) {
	switch ev.Kind {
	case kindStage:
		c.idxStage(ev)
	case kindStageKick:
		c.idxKick(ev)
	case kindQueue:
		c.idxQueue(ev)
	case kindSkip:
		c.idxSkip(ev)
	case kindAutoDJ:
		c.idxAutoDJ(ev)
	case kindAutoDJCtrl:
		c.idxAutoDJCtrl(ev)
	}
}

func (c *conductor) idxStage(ev *nostr.Event) {
	club := tagVal(ev, "h")
	if club == "" {
		return
	}
	lastSeen := int64(ev.CreatedAt) * 1000
	since := int64(ev.CreatedAt)
	if s := tagVal(ev, "since"); s != "" {
		if v, err := strconv.ParseInt(s, 10, 64); err == nil {
			since = v
		}
	}
	entry := stageEntry{since: since, lastSeen: lastSeen, on: ev.Content != "off"}
	c.idxMu.Lock()
	m := c.stageIdx[club]
	if m == nil {
		m = map[string]stageEntry{}
		c.stageIdx[club] = m
	}
	if ex, ok := m[ev.PubKey]; !ok || lastSeen > ex.lastSeen {
		m[ev.PubKey] = entry
		log.Printf("conductor [%.8s] stage dj=%.8s on=%v since=%d createdAt=%d", club, ev.PubKey, entry.on, since, ev.CreatedAt)
	}
	c.idxMu.Unlock()
}

func (c *conductor) idxKick(ev *nostr.Event) {
	club := tagVal(ev, "h")
	dj := tagVal(ev, "d")
	if club == "" || dj == "" {
		return
	}
	ms := int64(ev.CreatedAt) * 1000
	c.idxMu.Lock()
	m := c.kickIdx[club]
	if m == nil {
		m = map[string]int64{}
		c.kickIdx[club] = m
	}
	if ms > m[dj] {
		m[dj] = ms
	}
	c.idxMu.Unlock()
}

func (c *conductor) idxQueue(ev *nostr.Event) {
	club := tagVal(ev, "h")
	if club == "" {
		return
	}
	// Relay-authored queue updates (markTrackOff) carry a "p" tag naming the actual DJ.
	djPk := ev.PubKey
	relayAuthored := ev.PubKey == c.pub
	if relayAuthored {
		p := tagVal(ev, "p")
		if p == "" {
			return // relay-authored queue without "p" tag — ignore
		}
		djPk = p
	}
	c.idxMu.Lock()
	m := c.queueIdx[club]
	if m == nil {
		m = map[string]*nostr.Event{}
		c.queueIdx[club] = m
	}
	accepted := false
	if ex, ok := m[djPk]; !ok || ev.CreatedAt > ex.CreatedAt {
		m[djPk] = ev
		accepted = true
	}
	c.idxMu.Unlock()

	if accepted {
		active, total := countQueueTracks(ev)
		log.Printf("conductor [%.8s] idxQueue dj=%.8s relay=%v createdAt=%d tracks=%d active=%d", club, djPk, relayAuthored, ev.CreatedAt, total, active)
	}

	// A DJ-authored event with active tracks signals that the bootstrap should retry
	// immediately instead of waiting out the condExhaustedBackoff window. Relay-authored
	// events only mark tracks off and never add new playable ones, so they're excluded.
	if !relayAuthored && queueHasActiveTracks(ev) {
		c.queueWakeup.Store(club, struct{}{})
	}
}

// queueHasActiveTracks returns true if the 30103 event has at least one track without "off".
func queueHasActiveTracks(ev *nostr.Event) bool {
	for _, t := range ev.Tags {
		if len(t) >= 2 && t[0] == "track" && (len(t) < 5 || t[4] != "off") {
			return true
		}
	}
	return false
}

// countQueueTracks returns (active, total) track counts for a 30103 event.
func countQueueTracks(ev *nostr.Event) (active, total int) {
	for _, t := range ev.Tags {
		if len(t) >= 2 && t[0] == "track" {
			total++
			if len(t) < 5 || t[4] != "off" {
				active++
			}
		}
	}
	return
}

func (c *conductor) idxSkip(ev *nostr.Event) {
	club := tagVal(ev, "h")
	if club == "" {
		return
	}
	c.idxMu.Lock()
	if ex, ok := c.skipIdx[club]; !ok || ev.CreatedAt > ex.CreatedAt {
		c.skipIdx[club] = ev
	}
	c.idxMu.Unlock()
}

func (c *conductor) idxAutoDJ(ev *nostr.Event) {
	club := tagVal(ev, "h")
	if club == "" {
		return
	}
	c.idxMu.Lock()
	if ex, ok := c.autoDJIdx[club]; !ok || ev.CreatedAt > ex.CreatedAt {
		c.autoDJIdx[club] = ev
	}
	c.idxMu.Unlock()
}

func (c *conductor) idxAutoDJCtrl(ev *nostr.Event) {
	if ev.PubKey != c.pub {
		return // only relay-signed disarm markers are trusted
	}
	club := tagVal(ev, "d")
	if club == "" {
		return
	}
	c.idxMu.Lock()
	if t, ok := c.autoDJCtrlIdx[club]; !ok || ev.CreatedAt > t {
		c.autoDJCtrlIdx[club] = ev.CreatedAt
	}
	c.idxMu.Unlock()
}

// clubOwner returns the pubkey of the club's creator (author of its 9007 create-group event).
// Lookup order: in-memory cache → SQLite → BadgerDB. Result is cached permanently.
func (c *conductor) clubOwner(ctx context.Context, club string) string {
	c.idxMu.Lock()
	if owner, ok := c.ownerCache[club]; ok {
		c.idxMu.Unlock()
		return owner
	}
	c.idxMu.Unlock()
	// SQLite cache (survives restarts, avoids BadgerDB scan on subsequent lookups).
	if owner := c.sqLoadOwner(club); owner != "" {
		c.idxMu.Lock()
		c.ownerCache[club] = owner
		c.idxMu.Unlock()
		return owner
	}
	// Fall back to BadgerDB (first lookup or SQLite disabled).
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
	c.sqSaveOwner(club, newest.PubKey)
	c.idxMu.Lock()
	c.ownerCache[club] = newest.PubKey
	c.idxMu.Unlock()
	return newest.PubKey
}

// ── SQLite persistence helpers ────────────────────────────────────────────────
// All called from the single conductor tick goroutine → no extra locking needed.
// c.sq == nil is a safe no-op (graceful degradation without SQLite).

func (c *conductor) sqSaveState(club string, pb *condClub) {
	if c.sq == nil {
		return
	}
	playing := 0
	if pb.playing {
		playing = 1
	}
	if _, err := c.sq.Exec(
		`INSERT OR REPLACE INTO conductor_state(club,pos,video_id,dj,title,duration,started_at,playing) VALUES(?,?,?,?,?,?,?,?)`,
		club, pb.pos, pb.videoID, pb.dj, pb.title, pb.duration, pb.startedAt, playing,
	); err != nil {
		log.Printf("sqlite save_state [%.8s]: %v", club, err)
	}
}

// sqStopState persists a stopped state (playing=false) and clears the played log.
// Replaces sqClearState: retaining the conductor_state row with playing=0 prevents the
// cold-start resume from falling through to BadgerDB, which would see the last video ID
// and incorrectly set playing=true, triggering an infinite bootstrap loop.
func (c *conductor) sqStopState(club string, pb *condClub) {
	c.sqSaveState(club, pb) // pb.playing must already be false (caller invoked stop() first)
	if c.sq == nil {
		return
	}
	if _, err := c.sq.Exec(`DELETE FROM played WHERE club=?`, club); err != nil {
		log.Printf("sqlite clear_played [%.8s]: %v", club, err)
	}
}

func (c *conductor) sqRecordPlayed(club, dj, videoID string, playedAt int64) {
	if c.sq == nil {
		return
	}
	log.Printf("conductor [%.8s] sqRecordPlayed dj=%.8s vid=%s pAt=%d", club, dj, videoID, playedAt)
	if _, err := c.sq.Exec(`INSERT OR REPLACE INTO played(club,dj,video_id,played_at) VALUES(?,?,?,?)`, club, dj, videoID, playedAt); err != nil {
		log.Printf("sqlite record_played [%.8s]: %v", club, err)
	}
}

func (c *conductor) sqClearPlayedForDJ(club, dj string) {
	if c.sq == nil {
		return
	}
	if _, err := c.sq.Exec(`DELETE FROM played WHERE club=? AND dj=?`, club, dj); err != nil {
		log.Printf("sqlite clear_played_dj [%.8s] dj=%.8s: %v", club, dj, err)
	}
}

// sqLoadState reads persisted conductor state for a club. Returns nil if none found.
func (c *conductor) sqLoadState(club string) *condClub {
	if c.sqr == nil {
		return nil
	}
	row := c.sqr.QueryRow(
		`SELECT pos,video_id,dj,title,duration,started_at,playing FROM conductor_state WHERE club=?`, club,
	)
	var pb condClub
	var playing int
	if err := row.Scan(&pb.pos, &pb.videoID, &pb.dj, &pb.title, &pb.duration, &pb.startedAt, &playing); err != nil {
		return nil
	}
	pb.playing = playing != 0
	return &pb
}

// sqLoadPlayed reads the persisted played-set for a club. Returns nil if empty or SQLite disabled.
func (c *conductor) sqLoadPlayed(club string) map[string]map[string]int64 {
	if c.sqr == nil {
		return nil
	}
	rows, err := c.sqr.Query(`SELECT dj, video_id, played_at FROM played WHERE club=?`, club)
	if err != nil {
		return nil
	}
	defer rows.Close()
	out := map[string]map[string]int64{}
	for rows.Next() {
		var dj, vid string
		var pAt int64
		if err := rows.Scan(&dj, &vid, &pAt); err != nil {
			continue
		}
		if out[dj] == nil {
			out[dj] = map[string]int64{}
		}
		out[dj][vid] = pAt
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// sqLoadNowPlaying reads the relay-authored now_playing event for a club from BadgerDB.
// Used on restart to restore mood counts. Returns nil if not found.
func (c *conductor) sqLoadNowPlaying(ctx context.Context, club string) *nostr.Event {
	ch, err := c.db.QueryEvents(ctx, nostr.Filter{Kinds: []int{kindNowPlaying}, Tags: nostr.TagMap{"h": []string{club}}})
	if err != nil {
		return nil
	}
	var latest *nostr.Event
	for ev := range ch {
		if ev.PubKey == c.pub && (latest == nil || ev.CreatedAt > latest.CreatedAt) {
			latest = ev
		}
	}
	return latest
}

// restoreMoodFromEvent seeds the in-memory mood maps from the mood_bangers/mood_skips tags
// of a now_playing event. Called on restart so votes accumulated before the restart survive.
// Uses aggregate sentinel keys (non-hex, impossible for real Nostr pubkeys) to avoid
// collisions with live per-pubkey votes.
func (c *conductor) restoreMoodFromEvent(club string, pos int, ev *nostr.Event) {
	bangers := atoiDefault(tagVal(ev, "mood_bangers"), 0)
	skips := atoiDefault(tagVal(ev, "mood_skips"), 0)
	if bangers == 0 && skips == 0 {
		return
	}
	c.moodMu.Lock()
	if bangers > 0 {
		if c.moods[club] == nil {
			c.moods[club] = map[int]map[string]string{}
		}
		if c.moods[club][pos] == nil {
			c.moods[club][pos] = map[string]string{}
		}
		for i := 0; i < bangers; i++ {
			c.moods[club][pos][fmt.Sprintf("_r%d_", i)] = "banger"
		}
	}
	if skips > 0 {
		if c.skipCounts[club] == nil {
			c.skipCounts[club] = map[int]map[string]int{}
		}
		if c.skipCounts[club][pos] == nil {
			c.skipCounts[club][pos] = map[string]int{}
		}
		c.skipCounts[club][pos]["_restored_"] = skips
	}
	c.moodMu.Unlock()
	log.Printf("conductor [%.8s] restored mood pos=%d bangers=%d skips=%d", club, pos, bangers, skips)
}

// sqSaveOwner persists a club→owner mapping (immutable once written, INSERT OR IGNORE).
func (c *conductor) sqSaveOwner(club, owner string) {
	if c.sq == nil {
		return
	}
	if _, err := c.sq.Exec(`INSERT OR IGNORE INTO club_owners(club,owner) VALUES(?,?)`, club, owner); err != nil {
		log.Printf("sqlite save_owner [%.8s]: %v", club, err)
	}
}

// sqLoadOwner returns the persisted owner for a club, or "" if not found.
func (c *conductor) sqLoadOwner(club string) string {
	if c.sqr == nil {
		return ""
	}
	var owner string
	if err := c.sqr.QueryRow(`SELECT owner FROM club_owners WHERE club=?`, club).Scan(&owner); err != nil {
		return ""
	}
	return owner
}

// ── stageGate in-memory helpers ───────────────────────────────────────────────

// countActiveOtherDJs counts DJs currently on stage in `club` (excluding `senderPubkey`),
// and reports whether `senderPubkey` is already on stage (= heartbeat, always allowed).
// Reads from the in-memory stageIdx + kickIdx — no DB access.
func (c *conductor) countActiveOtherDJs(club, senderPubkey string) (active int, alreadyOnStage bool) {
	now := time.Now().UnixMilli()
	staleThreshold := now - condStageStaleMS
	c.idxMu.Lock()
	stageMap := c.stageIdx[club]
	kickMap := c.kickIdx[club]
	c.idxMu.Unlock()
	for pk, entry := range stageMap {
		if pk == senderPubkey {
			alreadyOnStage = entry.on &&
				entry.lastSeen >= staleThreshold &&
				(kickMap[pk] == 0 || entry.lastSeen > kickMap[pk])
			continue
		}
		if !entry.on {
			continue
		}
		if entry.lastSeen < staleThreshold {
			continue
		}
		if km := kickMap[pk]; km > 0 && entry.lastSeen <= km {
			continue
		}
		active++
	}
	return
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

// observeMood records a vibe vote (kind 20104) from a club member.
// Tags: h=club, pos=track-pos (int), v=banger|skip. One vote per (club, pos, pubkey) counted.
func (c *conductor) observeMood(_ context.Context, ev *nostr.Event) {
	if ev.Kind != kindMood {
		return
	}
	club := tagVal(ev, "h")
	posStr := tagVal(ev, "pos")
	vote := tagVal(ev, "v")
	if club == "" || posStr == "" || (vote != "banger" && vote != "skip") {
		return
	}
	pos, err := strconv.Atoi(posStr)
	if err != nil || pos < 0 {
		return
	}
	c.moodMu.Lock()
	if vote == "banger" {
		if c.moods[club] == nil {
			c.moods[club] = map[int]map[string]string{}
		}
		if c.moods[club][pos] == nil {
			c.moods[club][pos] = map[string]string{}
		}
		c.moods[club][pos][ev.PubKey] = "banger"
	} else {
		// skip: accumulate per-pubkey (one user can press skip up to moodSkipThreshold times)
		if c.skipCounts[club] == nil {
			c.skipCounts[club] = map[int]map[string]int{}
		}
		if c.skipCounts[club][pos] == nil {
			c.skipCounts[club][pos] = map[string]int{}
		}
		if c.skipCounts[club][pos][ev.PubKey] < moodSkipThreshold {
			c.skipCounts[club][pos][ev.PubKey]++
		}
	}
	c.moodMu.Unlock()
}

// moodCounts returns the banger and skip vote counts for club/pos (caller must NOT hold moodMu).
// Bangers are distinct per pubkey; skips accumulate (one user may vote skip up to moodSkipThreshold).
func (c *conductor) moodCounts(club string, pos int) (bangers, skips int) {
	c.moodMu.Lock()
	for _, v := range c.moods[club][pos] {
		if v == "banger" {
			bangers++
		}
	}
	for _, cnt := range c.skipCounts[club][pos] {
		skips += cnt
	}
	c.moodMu.Unlock()
	return
}

// moodSkip reports whether ≥ moodSkipThreshold distinct members voted "skip" for the current track.
func (c *conductor) moodSkip(club string, pb *condClub) bool {
	if !pb.playing {
		return false
	}
	_, skips := c.moodCounts(club, pb.pos)
	return skips >= moodSkipThreshold
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

// recordBrokenVid remembers a broken-SKIPPED video so the matrix keeps it out of the
// rotation for brokenVidBlockMS — the 2-min report window alone can't stop the loop:
// with 2 DJs the round-robin returns ~4 min later, the quorum has expired, and the
// unplayable track starts (and gets skipped) again every single round. A stale client
// tab that republishes its queue with the track re-activated can't resurrect it either.
func (c *conductor) recordBrokenVid(club, vid string, now int64) {
	if vid == "" {
		return
	}
	c.brokenMu.Lock()
	if c.brokenVids[club] == nil {
		c.brokenVids[club] = map[string]int64{}
	}
	c.brokenVids[club][vid] = now
	c.brokenMu.Unlock()
}

// isBrokenVid reports whether vid was broken-skipped in this club within brokenVidBlockMS.
func (c *conductor) isBrokenVid(club, vid string, now int64) bool {
	c.brokenMu.Lock()
	defer c.brokenMu.Unlock()
	ts, ok := c.brokenVids[club][vid]
	return ok && now-ts < brokenVidBlockMS
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
	for club, vids := range c.brokenVids {
		for vid, ts := range vids {
			if now-ts >= brokenVidBlockMS {
				delete(vids, vid)
			}
		}
		if len(vids) == 0 {
			delete(c.brokenVids, club)
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
	t := int64(ev.CreatedAt) * 1000
	if t > m[ev.PubKey] {
		prev := m[ev.PubKey]
		m[ev.PubKey] = t
		// Log only when first seen or returning after the online window expired.
		if prev == 0 || t-prev > condOnlineMS {
			log.Printf("conductor [%.8s] presence dj=%.8s online (gap %dms)", club, ev.PubKey, t-prev)
		}
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

// setVirtualOnline marks a pubkey as present for the current tick so matrix() treats it as
// online and never applies the played-set to its tracks. Used for auto-DJ owners so their
// queue loops naturally without draining.
func (c *conductor) setVirtualOnline(club, pk string, now int64) {
	c.presMu.Lock()
	if c.pres[club] == nil {
		c.pres[club] = map[string]int64{}
	}
	c.pres[club][pk] = now
	c.presMu.Unlock()
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
	// Auto DJ: clubs whose owner armed a playlist. Included regardless of real DJs.
	auto := c.armedAutoClubs(ctx)

	c.mu.Lock()
	defer c.mu.Unlock()
	now := time.Now().UnixMilli()
	c.prunePresence(now)
	c.pruneBroken(now)
	// Forget state for clubs with no active real DJ AND no auto-DJ.
	// Tracks always play to completion — only an explicit skip interrupts them.
	for club := range c.clubs {
		if _, ok := active[club]; ok {
			continue
		}
		if _, ok := auto[club]; ok {
			continue
		}
		pb := c.clubs[club]
		if pb == nil {
			delete(c.clubs, club)
			continue
		}
		if pb.playing {
			dur := pb.duration
			if dur <= 0 {
				dur = condMaxTrackFallbackS
			}
			if (now-pb.startedAt)/1000 < int64(dur) {
				// Track still within its duration — keep heartbeating so clients
				// continue playback even though all DJs left the stage.
				if now-pb.lastBeat >= condHeartbeatMS {
					c.publishNowPlaying(ctx, club, pb, now)
				}
				continue // keep club state alive
			}
			// Track finished — stop and fall through to delete.
			c.stop(ctx, club, pb, now)
			c.sqSaveState(club, pb)
		}
		delete(c.clubs, club)
	}
	// Real DJs take full priority — Auto DJ is a fallback only, never injected alongside.
	for club, djs := range active {
		c.driveClub(ctx, club, djs, nil, now)
	}
	for club, st := range auto {
		if _, hasRealDJ := active[club]; hasRealDJ {
			continue // real DJs on stage → auto-DJ stands down
		}
		// Solo auto-DJ mode: no real DJs on stage.
		c.driveAutoClub(ctx, club, st, now)
	}
}

// ── reading club state from the store ────────────────────────────────────────

// activeClubs returns, per club with ≥1 active stage DJ, the DJ list in round-robin order
// (oldest `since` first, pubkey tiebreak, capped) — the same selection as conductor.ts
// selectActiveDjs: on + fresh (<1h) + not kicked. Reads from the in-memory indexes (no DB).
func (c *conductor) activeClubs(ctx context.Context) map[string][]condDJ {
	now := time.Now().UnixMilli()
	// Snapshot indexes under lock, then process without holding it.
	type djSnap struct {
		pubkey string
		entry  stageEntry
		kickMs int64 // newest kick for this dj in this club, or 0
	}
	c.idxMu.Lock()
	type clubSnap struct{ djs []djSnap }
	snaps := make(map[string]clubSnap, len(c.stageIdx))
	for club, djMap := range c.stageIdx {
		var djs []djSnap
		km := c.kickIdx[club]
		for pk, e := range djMap {
			djs = append(djs, djSnap{pubkey: pk, entry: e, kickMs: km[pk]})
		}
		snaps[club] = clubSnap{djs: djs}
	}
	c.idxMu.Unlock()

	out := map[string][]condDJ{}
	for club, snap := range snaps {
		var list []condDJ
		for _, se := range snap.djs {
			if !se.entry.on || now-se.entry.lastSeen >= condStageStaleMS {
				continue
			}
			if se.kickMs > 0 && se.entry.lastSeen <= se.kickMs {
				continue // kicked after their last heartbeat
			}
			list = append(list, condDJ{pubkey: se.pubkey, since: se.entry.since})
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

// clubQueues returns the newest queue (kind 30103) per DJ for a club, parsed to tracks.
// Reads from the in-memory index (no DB).
func (c *conductor) clubQueues(_ context.Context, club string) map[string][]condTrack {
	c.idxMu.Lock()
	m := c.queueIdx[club]
	evs := make([]*nostr.Event, 0, len(m))
	for _, ev := range m {
		evs = append(evs, ev)
	}
	c.idxMu.Unlock()
	out := map[string][]condTrack{}
	outTime := map[string]nostr.Timestamp{}
	outRelay := map[string]bool{}
	for _, ev := range evs {
		// relay-authored queue updates (markTrackOff) carry a "p" tag; use that as the DJ key.
		key := ev.PubKey
		relayOwned := ev.PubKey == c.pub
		if relayOwned {
			if p := tagVal(ev, "p"); p != "" {
				key = p
			}
		}
		// When a DJ re-adds a track after markTrackOff, the DJ's newer event must win over
		// the relay's older off-patch; use the most recent CreatedAt to break the conflict.
		if prev, has := outTime[key]; !has || ev.CreatedAt > prev {
			out[key] = parseQueueTracks(ev)
			outTime[key] = ev.CreatedAt
			outRelay[key] = relayOwned
		}
	}
	for djPk, tracks := range out {
		active := 0
		for _, t := range tracks {
			if t.active {
				active++
			}
		}
		// This runs on every conductor tick (~2.5 s) per DJ per club — log only when the
		// queue state actually changed, otherwise the journal fills with identical lines.
		fp := fmt.Sprintf("%d:%d:%d:%v", len(tracks), active, outTime[djPk], outRelay[djPk])
		key := club + ":" + djPk
		c.qLogMu.Lock()
		changed := c.qLogged[key] != fp
		if changed {
			c.qLogged[key] = fp
		}
		c.qLogMu.Unlock()
		if changed {
			log.Printf("conductor [%.8s] queue dj=%.8s tracks=%d active=%d created=%d relay=%v",
				club, djPk, len(tracks), active, outTime[djPk], outRelay[djPk])
		}
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

// driveClub drives a club in real-DJ round-robin mode. extraQueues, if non-nil, injects
// additional per-DJ track lists (e.g. auto-DJ virtual tracks) into the queue map.
func (c *conductor) driveClub(ctx context.Context, club string, djs []condDJ, extraQueues map[string][]condTrack, now int64) {
	queues := c.clubQueues(ctx, club)
	for pk, tracks := range extraQueues {
		queues[pk] = tracks
	}
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
	// Throttled via a conductor-level map (survives condClub deletion/recreation by the
	// tick cleanup loop) to avoid busy-spinning when all queues are empty.
	if !pb.playing {
		// A new DJ-authored 30103 with active tracks arrived — reset backoff immediately.
		if _, woke := c.queueWakeup.LoadAndDelete(club); woke {
			c.bootstrapAt[club] = 0
		}
		if now-c.bootstrapAt[club] >= condHeartbeatMS {
			c.bootstrapAt[club] = now
			log.Printf("conductor [%.8s] advance reason=bootstrap pos=%d", club, pb.pos)
			c.advance(ctx, club, djPks, queues, pb, now)
		}
		return
	}
	// Orphan guard: the playing DJ left the stage.
	// Let the current track finish its natural duration first — it may still be
	// streaming live — and only advance once the track time has elapsed.
	if pb.dj != "" && indexOf(djPks, pb.dj) < 0 {
		dur := pb.duration
		if dur <= 0 {
			dur = condMaxTrackFallbackS
		}
		if (now-pb.startedAt)/1000 < int64(dur) {
			// Track still running — keep heartbeating; duration check below advances.
			if now-pb.lastBeat >= condHeartbeatMS {
				c.publishNowPlaying(ctx, club, pb, now)
			}
			return
		}
		log.Printf("conductor [%.8s] advance reason=orphan dj=%.8s", club, pb.dj)
		c.advance(ctx, club, djPks, queues, pb, now)
		return
	}
	// Skip requested (kind 30107, owner/mod/playing-DJ — validated in skipRequested).
	if c.skipRequested(ctx, club, pb) {
		log.Printf("conductor [%.8s] advance reason=skip pos=%d", club, pb.pos)
		c.advance(ctx, club, djPks, queues, pb, now)
		return
	}
	// Track reported unplayable (kind 20102) by an authorized user or a quorum of members.
	// Rate-limited to once per heartbeat so a fully-broken queue drains slowly (giving the DJ
	// time to react) rather than flushing all tracks in seconds.
	if c.brokenSkip(club, pb, now) {
		if now-c.brokenSkipAt[club] >= condHeartbeatMS {
			c.brokenSkipAt[club] = now
			log.Printf("conductor [%.8s] advance reason=broken vid=%s", club, pb.videoID)
			c.recordBrokenVid(club, pb.videoID, now)
			c.advance(ctx, club, djPks, queues, pb, now)
			return
		}
	}
	// Vibe vote: ≥ moodSkipThreshold distinct members voted "skip" (kind 20104).
	if c.moodSkip(club, pb) {
		if now-c.moodSkipAt[club] >= condHeartbeatMS {
			c.moodSkipAt[club] = now
			log.Printf("conductor [%.8s] advance reason=mood-skip pos=%d", club, pb.pos)
			c.advance(ctx, club, djPks, queues, pb, now)
			return
		}
	}
	// Track finished (fallback cap when duration unknown)?
	dur := pb.duration
	if dur <= 0 {
		dur = condMaxTrackFallbackS
	}
	if (now-pb.startedAt)/1000 >= int64(dur) {
		log.Printf("conductor [%.8s] advance reason=duration elapsed=%ds dur=%ds vid=%s", club, (now-pb.startedAt)/1000, dur, pb.videoID)
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
		c.stop(ctx, club, pb, now)
		delete(c.played, club)
		c.sqStopState(club, pb)
		return
	}
	mat := c.matrix(djPks, queues, pb, club, now)
	playableTotal := 0
	for _, row := range mat {
		for _, b := range row {
			if b {
				playableTotal++
			}
		}
	}
	di, ti := fairNext(djPks, pb.dj, mat)
	log.Printf("conductor [%.8s] advance fairNext djs=%d playableTotal=%d di=%d ti=%d lastDJ=%.8s", club, len(djPks), playableTotal, di, ti, pb.dj)
	if di == -1 {
		// Every DJ's queue is fully played-off (or empty) → stop to the lobby. No auto-loop: a set
		// plays through once, then the lobby runs until a DJ adds / re-activates tracks.
		log.Printf("conductor [%.8s] stop: no playable track djs=%d", club, len(djPks))
		c.stop(ctx, club, pb, now)
		// Keep the played-set intact while DJs are still on stage. Clearing it here would
		// unblock tracks that were deliberately played (e.g. stale 30103 from a previous
		// session) and let them replay immediately via the next bootstrap. Online DJs have
		// their browser fire reactivateMyQueue (new 30103, qAddedMs > pAt) to loop; offline
		// DJs are handled per-DJ by the loop-reset in matrix(). The club-wide delete belongs
		// only in the len(djPks)==0 path above (true session end), not here.
		c.sqSaveState(club, pb) // persist playing=false; keep played rows
		// Back off: all queues are empty, so retry in condExhaustedBackoff instead of
		// condHeartbeatMS. driveClub set bootstrapAt=now before calling advance(); override
		// it to suppress rapid retries while still checking again after a reasonable delay.
		// A new 30103 event resets queueIdx — the next retry will find the new tracks.
		c.bootstrapAt[club] = now + (condExhaustedBackoff - condHeartbeatMS)
		return
	}
	tracks := queues[djPks[di]]
	if ti >= len(tracks) {
		log.Printf("conductor [%.8s] stop: track index out of range di=%d ti=%d", club, di, ti)
		c.stop(ctx, club, pb, now)
		c.sqStopState(club, pb)
		return
	}
	t := tracks[ti]
	pb.pos++
	log.Printf("conductor [%.8s] play pos=%d dj=%.8s vid=%s dur=%d", club, pb.pos, djPks[di], t.videoID, t.duration)
	pb.dj = djPks[di]
	pb.videoID = t.videoID
	pb.title = t.title
	pb.duration = t.duration
	pb.startedAt = now
	pb.playing = true
	// Always mark the track off in the DJ's queue via a relay-signed event and record it in the
	// played-set. For offline/throttled DJs (browser tab inactive), this drains the queue to the
	// lobby — the browser never fires reactivateMyQueue so the exhausted queue stays exhausted.
	// For active online DJs, the browser also marks the track off independently; when all tracks
	// are off their reactivateMyQueue fires a new DJ-signed 30103 (all on, newer timestamp) which
	// supersedes the relay version and the loop continues normally.
	if c.played[club] == nil {
		c.played[club] = map[string]map[string]int64{}
	}
	if c.played[club][pb.dj] == nil {
		c.played[club][pb.dj] = map[string]int64{}
	}
	c.played[club][pb.dj][t.videoID] = now
	c.sqRecordPlayed(club, pb.dj, t.videoID, now)
	c.markTrackOff(ctx, club, pb.dj, t.videoID)
	c.publishNowPlaying(ctx, club, pb, now)
	c.publishPlay(ctx, club, pb, now)
	c.sqSaveState(club, pb)
	// Prune mood votes older than 2 positions to keep the maps bounded.
	c.moodMu.Lock()
	if cm := c.moods[club]; cm != nil {
		for p := range cm {
			if p < pb.pos-2 {
				delete(cm, p)
			}
		}
	}
	if sc := c.skipCounts[club]; sc != nil {
		for p := range sc {
			if p < pb.pos-2 {
				delete(sc, p)
			}
		}
	}
	c.moodMu.Unlock()
}

// matrix builds the playability matrix. A track is playable if it is `active` (NOT marked `off`)
// and not the currently-playing one.
//
// Played-set guard (all DJs): a track is additionally excluded when it was played AFTER the DJ's
// current 30103 queue event was authored (pAt > 0 && qAddedMs <= pAt). This timestamp comparison
// distinguishes a stale-tab republish (old 30103, qAddedMs < pAt → blocked) from a legitimate
// re-add (new 30103 after play, qAddedMs > pAt → playable). Relay-managed queues use off-flags
// as truth and skip the played-set check.
//
// Loop-reset for offline DJs: when every active track is blocked, the playlist has cycled once.
// Clear the played-set so it loops rather than stalling forever.
func (c *conductor) matrix(djPks []string, queues map[string][]condTrack, pb *condClub, club string, now int64) [][]bool {
	out := make([][]bool, len(djPks))
	for i, pk := range djPks {
		tracks := queues[pk]
		row := make([]bool, len(tracks))
		djOnline := c.online(club, pk, now)
		playedByDJ := c.played[club][pk] // nil-safe: map lookup on nil map is zero value

		// Grab relayManaged + qAddedMs under a single lock acquisition.
		var relayManaged bool
		var qAddedMs int64
		c.idxMu.Lock()
		if c.queueIdx[club] != nil {
			if ev := c.queueIdx[club][pk]; ev != nil {
				relayManaged = ev.PubKey == c.pub
				qAddedMs = int64(ev.CreatedAt) * 1000
			}
		}
		c.idxMu.Unlock()

		// pb.videoID exclusion: while playing (pb.playing=true), exclude the current track to
		// prevent picking it again mid-song. After a stop, skip the exclusion — markTrackOff
		// already set active=false, so the off-flag provides the loop guard. Keeping the
		// exclusion after stop would block re-adds (wasOff→active again must be playable).
		playable := 0
		for j, t := range tracks {
			notCurrent := !pb.playing || t.videoID != pb.videoID
			// A track is blocked when it was played after the DJ's current queue version was
			// authored. A legitimate re-add sends a newer 30103 (qAddedMs > pAt → not blocked).
			pAt := playedByDJ[t.videoID]
			playedOff := !relayManaged && pAt > 0 && qAddedMs <= pAt
			brokenVid := c.isBrokenVid(club, t.videoID, now)
			row[j] = t.active && notCurrent && !playedOff && !brokenVid
			if row[j] {
				playable++
			} else {
				switch {
				case !t.active:
					log.Printf("conductor [%.8s] matrix blocked dj=%.8s vid=%s reason=inactive", club, pk, t.videoID)
				case !notCurrent:
					log.Printf("conductor [%.8s] matrix blocked dj=%.8s vid=%s reason=current", club, pk, t.videoID)
				case playedOff:
					log.Printf("conductor [%.8s] matrix blocked dj=%.8s vid=%s reason=played pAt=%d qAdded=%d", club, pk, t.videoID, pAt, qAddedMs)
				case brokenVid:
					log.Printf("conductor [%.8s] matrix blocked dj=%.8s vid=%s reason=broken", club, pk, t.videoID)
				}
			}
		}
		log.Printf("conductor [%.8s] matrix dj=%.8s online=%v relayManaged=%v qAdded=%d tracks=%d playable=%d", club, pk, djOnline, relayManaged, qAddedMs, len(tracks), playable)

		// For offline unmanaged DJs: if every active (non-playing) track is blocked by the
		// played-set, the playlist has cycled through once. Clear it so the queue loops.
		if !djOnline && !relayManaged {
			wouldPlay, blocked := 0, 0
			for _, t := range tracks {
				notCurrent := !pb.playing || t.videoID != pb.videoID
				if t.active && notCurrent {
					wouldPlay++
					pAt := playedByDJ[t.videoID]
					if pAt > 0 && qAddedMs <= pAt {
						blocked++
					}
				}
			}
			if wouldPlay > 0 && blocked == wouldPlay {
				log.Printf("conductor [%.8s] matrix loop-reset dj=%.8s: %d/%d tracks played, clearing played-set", club, pk, blocked, wouldPlay)
				if c.played[club] != nil {
					delete(c.played[club], pk)
				}
				c.sqClearPlayedForDJ(club, pk)
				for j, t := range tracks {
					notCurrent := !pb.playing || t.videoID != pb.videoID
					row[j] = t.active && notCurrent
				}
			}
		}
		out[i] = row
	}
	return out
}

// skipRequested reports whether a fresh, AUTHORIZED skip-request (kind 30107) targets the
// running track. The request must (a) name the current pos, (b) be newer than the current
// track's start (so a stale request from a previous track is ignored), and (c) come from a club
// owner/moderator or the currently-playing DJ. Reads from the in-memory index (no DB).
func (c *conductor) skipRequested(_ context.Context, club string, pb *condClub) bool {
	if !pb.playing {
		return false
	}
	c.idxMu.Lock()
	latest := c.skipIdx[club]
	c.idxMu.Unlock()
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

func (c *conductor) stop(ctx context.Context, club string, pb *condClub, now int64) {
	// Publish a paused now_playing so clients switch to the lobby immediately instead of waiting
	// for the event to go stale (LIVE_STALE_MS = 150 s). Only send if a track was ever playing
	// (videoID non-empty); on a cold start there is nothing to retract.
	//
	// KEEP pb.videoID/dj — do NOT clear them. The next tick re-runs advance() (bootstrap), and the
	// matrix excludes pb.videoID. If we cleared it, the just-played final track would no longer be
	// excluded; when the DJ's client hasn't marked it `off` yet (e.g. they closed the tab as it
	// ended), the bootstrap would re-pick it and the LAST SONG WOULD LOOP. Retaining it holds that
	// one track back until a different/new active track plays (which changes pb.videoID) — so a set
	// plays through once, then the lobby runs. No auto-loop.
	if pb.videoID != "" {
		c.publishNowPlayingPaused(ctx, club, pb, now)
	}
	pb.playing = false
}

// ── publishing (relay-signed, straight to the store, bypassing RejectEvent) ───

// publishNowPlayingPaused republishes the current state as paused so clients switch to
// the lobby immediately. A fresh wall-clock timestamp avoids a same-second collision with
// the last playing heartbeat, which ReplaceEvent would reject as not newer.
func (c *conductor) publishNowPlayingPaused(ctx context.Context, club string, pb *condClub, _ int64) {
	now := time.Now().UnixMilli()
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

func (c *conductor) publishNowPlaying(ctx context.Context, club string, pb *condClub, now int64) {
	bangers, skips := c.moodCounts(club, pb.pos)
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
			{"mood_bangers", strconv.Itoa(bangers)},
			{"mood_skips", strconv.Itoa(skips)},
		},
		Content: pb.title,
	}
	pb.lastBeat = now
	if bangers > 0 || skips > 0 {
		log.Printf("conductor [%.8s] mood pos=%d bangers=%d skips=%d", club, pb.pos, bangers, skips)
	}
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
// Retries up to 3× on badger transaction conflicts (transient, safe to retry immediately).
func (c *conductor) publish(ctx context.Context, ev *nostr.Event, replace bool) {
	if err := ev.Sign(c.sk); err != nil {
		log.Printf("conductor sign kind %d: %v", ev.Kind, err)
		return
	}
	for attempt := 0; attempt < 3; attempt++ {
		var err error
		if replace {
			err = c.db.ReplaceEvent(ctx, ev)
		} else {
			err = c.db.SaveEvent(ctx, ev)
		}
		if err == nil {
			c.relay.BroadcastEvent(ev)
			return
		}
		if attempt < 2 && strings.Contains(err.Error(), "Conflict") {
			time.Sleep(time.Duration(attempt+1) * 10 * time.Millisecond)
			continue
		}
		log.Printf("conductor store kind %d: %v", ev.Kind, err)
		return
	}
}

// ── cold-start resume ─────────────────────────────────────────────────────────

// resume rebuilds a club's playback state after a relay restart from the newest relay-authored
// now_playing (continue the current track). Also seeds the played-set from recent 1313 records
// so offline DJs' queues don't replay tracks from before the restart.
// Best-effort: if no relay now_playing exists, returns an empty state (the next tick bootstraps).
func (c *conductor) resume(ctx context.Context, club string) *condClub {
	// Fast path: SQLite has the last persisted state (survives restarts without event replay).
	if sq := c.sqLoadState(club); sq != nil {
		// Guard against wildly stale state (e.g. relay crash + restart hours later):
		// if more than duration + 2h has passed since started_at, the session is over.
		// Reset to not-playing so bootstrap runs cleanly instead of immediately firing
		// "advance reason=duration elapsed=Ns" with elapsed >> duration.
		if sq.playing {
			now := time.Now().UnixMilli()
			elapsed := (now - sq.startedAt) / 1000
			threshold := int64(sq.duration)
			if threshold <= 0 {
				threshold = condMaxTrackFallbackS
			}
			threshold += 7200 // +2h slack for legitimately long or paused streams
			if elapsed > threshold {
				log.Printf("conductor [%.8s] resume: stale state cleared (elapsed=%ds dur=%ds started=%d) — resetting to not-playing",
					club, elapsed, sq.duration, sq.startedAt/1000)
				sq.playing = false
				sq.videoID = ""
			}
		}
		log.Printf("conductor [%.8s] resume: sqlite pos=%d playing=%v", club, sq.pos, sq.playing)
		if played := c.sqLoadPlayed(club); played != nil {
			c.played[club] = played
		}
		// Restore mood counts from the last now_playing so skip-votes survive a restart.
		if np := c.sqLoadNowPlaying(ctx, club); np != nil {
			c.restoreMoodFromEvent(club, sq.pos, np)
		}
		return sq
	}

	// Fallback: no SQLite data (first boot or SQLite disabled) → replay BadgerDB events.
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
	// Restore mood counts from the last heartbeat so skip-votes survive a restart.
	c.restoreMoodFromEvent(club, pb.pos, latest)
	log.Printf("conductor [%.8s] resume: badger pos=%d playing=%v (sqlite empty)", club, pb.pos, pb.playing)

	// Seed played-set from 1313 play-log so offline DJs don't replay after a restart.
	// Only applies while their browser hasn't sent presence yet (online() = false).
	ch2, err := c.db.QueryEvents(ctx, nostr.Filter{Kinds: []int{kindPlay}, Tags: nostr.TagMap{"h": []string{club}}})
	if err == nil {
		for ev := range ch2 {
			dj := tagVal(ev, "p")
			vid := ev.Content
			if dj == "" || vid == "" {
				continue
			}
			if c.played[club] == nil {
				c.played[club] = map[string]map[string]int64{}
			}
			if c.played[club][dj] == nil {
				c.played[club][dj] = map[string]int64{}
			}
			c.played[club][dj][vid] = int64(ev.CreatedAt) * 1000
		}
	}
	// Persist to SQLite so the next restart uses the fast path.
	c.sqSaveState(club, pb)
	for dj, vids := range c.played[club] {
		for vid, pAt := range vids {
			c.sqRecordPlayed(club, dj, vid, pAt)
		}
	}
	return pb
}

// markTrackOff publishes a relay-signed kind-30103 queue update that marks videoID as `off`
// (played/disabled). Called for every DJ after each track play — online or offline. The conductor
// uses its relay key (no DJ private key needed). The event is indexed in queueIdx under the DJ's
// pubkey (via the "p" tag). For offline/throttled DJs this drains the queue to the lobby. For
// active online DJs the browser publishes its own reactivateMyQueue (all on, newer timestamp)
// which supersedes this relay version, letting the loop continue.
func (c *conductor) markTrackOff(ctx context.Context, club, djPk, videoID string) {
	c.idxMu.Lock()
	var ev *nostr.Event
	if m := c.queueIdx[club]; m != nil {
		ev = m[djPk]
	}
	c.idxMu.Unlock()
	if ev == nil {
		return
	}
	// Build new tag list, marking the played track off.
	newTags := nostr.Tags{
		{"h", club},
		{"d", djPk + ":" + club}, // unique d-tag per (relay, DJ, club)
		{"p", djPk},
	}
	marked := false
	for _, t := range ev.Tags {
		if len(t) < 2 || t[0] != "track" {
			continue
		}
		tag := make(nostr.Tag, len(t))
		copy(tag, t)
		vid := strings.TrimPrefix(tag[1], "yt:")
		if vid == videoID && (len(tag) < 5 || tag[4] != "off") {
			for len(tag) < 5 {
				tag = append(tag, "")
			}
			tag[4] = "off"
			marked = true
		}
		newTags = append(newTags, tag)
	}
	if !marked {
		return
	}
	updated := &nostr.Event{
		Kind:      kindQueue,
		CreatedAt: nostr.Timestamp(time.Now().Unix()),
		Tags:      newTags,
		Content:   ev.Content,
	}
	log.Printf("conductor [%.8s] markTrackOff dj=%.8s vid=%s", club, djPk, videoID)
	c.publish(ctx, updated, true)

	// Check if all tracks are now off (playlist exhausted).
	allOff := true
	for _, t := range newTags {
		if t[0] == "track" && (len(t) < 5 || t[4] != "off") {
			allOff = false
			break
		}
	}
	// Relay-signed events bypass OnEventSaved; update the queue index directly so
	// relayManaged=true takes effect immediately for the next advance().
	c.idxQueue(updated)
	if allOff {
		// Queue exhausted — don't loop. Clear the played-set so tracks can replay
		// if the DJ re-adds them, then let the matrix naturally skip this DJ
		// (all tracks are off) until fresh tracks arrive via a new queue event.
		log.Printf("conductor [%.8s] queue exhausted dj=%.8s", club, djPk)
		if c.played[club] != nil {
			delete(c.played[club], djPk)
		}
		c.sqClearPlayedForDJ(club, djPk)
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

// armedAutoClubs returns all clubs with an owner-armed Auto DJ playlist, regardless of whether
// real DJs are on stage. The caller decides how to integrate auto-DJ (round-robin vs solo).
// Reads from the in-memory indexes (no DB).
func (c *conductor) armedAutoClubs(ctx context.Context) map[string]*autoState {
	type armedEntry struct {
		ev    *nostr.Event
		owner string
	}

	c.idxMu.Lock()
	armed := map[string]armedEntry{}
	for club, ev := range c.autoDJIdx {
		if tagVal(ev, "status") == "armed" {
			armed[club] = armedEntry{ev: ev, owner: ev.PubKey}
		}
	}
	disarmedAt := make(map[string]nostr.Timestamp, len(c.autoDJCtrlIdx))
	for club, t := range c.autoDJCtrlIdx {
		disarmedAt[club] = t
	}
	c.idxMu.Unlock()

	if len(armed) == 0 {
		return nil
	}

	out := map[string]*autoState{}
	for club, entry := range armed {
		if disarmedAt[club] >= entry.ev.CreatedAt {
			continue // manually disarmed; owner must re-arm
		}
		tracks := parseQueueTracks(entry.ev)
		if len(tracks) == 0 {
			continue
		}
		out[club] = &autoState{
			owner:  entry.owner,
			tracks: tracks,
			since:  int64(entry.ev.CreatedAt) * 1000,
		}
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

	if c.skipRequested(ctx, club, pb) {
		nextTrack()
		startTrack()
		return
	}
	if c.brokenSkip(club, pb, now) && now-c.brokenSkipAt[club] >= condHeartbeatMS {
		c.brokenSkipAt[club] = now
		log.Printf("conductor [%.8s] advance reason=broken-auto vid=%s", club, pb.videoID)
		c.recordBrokenVid(club, pb.videoID, now)
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
// over an armed-auto club. Replaceable per club (d=club). Also updates the ctrl index
// directly (relay-signed events bypass OnEventSaved so can't be indexed via observeEvent).
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
	// Update the ctrl index directly since relay-signed writes bypass OnEventSaved.
	c.idxMu.Lock()
	if t, ok := c.autoDJCtrlIdx[club]; !ok || ev.CreatedAt > t {
		c.autoDJCtrlIdx[club] = ev.CreatedAt
	}
	c.idxMu.Unlock()
}

// ── stageGate: enforce per-club DJ slot cap on kind-30102 events ─────────────

type stageGate struct {
	db         *badger.BadgerBackend
	superadmin string
	// Set after the conductor is initialized (main.go) so reject() reads
	// the conductor's in-memory indexes instead of querying BadgerDB.
	countFn func(club, sender string) (active int, alreadyOnStage bool)
}

// reject blocks a stage-join (kind 30102, content != "off") if the club is
// already at its DJ cap of 5.
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

	// Count active DJs via the conductor's in-memory index (zero DB access).
	// Falls back to a full BadgerDB scan if the conductor isn't wired in yet (startup race).
	active := 0
	alreadyOnStage := false
	if g.countFn != nil {
		active, alreadyOnStage = g.countFn(club, evt.PubKey)
	} else {
		// Fallback: query BadgerDB (used only during the brief startup window before
		// main.go wires g.countFn, or in tests).
		ch, err := g.db.QueryEvents(ctx, nostr.Filter{
			Kinds: []int{kindStage},
			Tags:  nostr.TagMap{"h": []string{club}},
		})
		if err != nil {
			return false, ""
		}
		newestByPk := map[string]*nostr.Event{}
		for ev := range ch {
			if ex, ok := newestByPk[ev.PubKey]; !ok || ev.CreatedAt > ex.CreatedAt {
				cp := *ev
				newestByPk[ev.PubKey] = &cp
			}
		}
		kickMs := map[string]int64{}
		if kch, err2 := g.db.QueryEvents(ctx, nostr.Filter{
			Kinds: []int{kindStageKick},
			Tags:  nostr.TagMap{"h": []string{club}},
		}); err2 == nil {
			for ev := range kch {
				target := tagVal(ev, "p")
				if target == "" {
					target = tagVal(ev, "d")
				}
				if target == "" {
					continue
				}
				ms := int64(ev.CreatedAt) * 1000
				if ms > kickMs[target] {
					kickMs[target] = ms
				}
			}
		}
		staleThreshold := time.Now().UnixMilli() - condStageStaleMS
		for pk, ev := range newestByPk {
			if pk == evt.PubKey {
				alreadyOnStage = true
				continue
			}
			if ev.Content == "off" {
				continue
			}
			evMs := int64(ev.CreatedAt) * 1000
			if evMs < staleThreshold {
				continue
			}
			if km := kickMs[pk]; km > 0 && evMs <= km {
				continue
			}
			active++
		}
	}
	if alreadyOnStage {
		return false, "" // heartbeat from existing DJ
	}

	if active >= condMaxDJs {
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
