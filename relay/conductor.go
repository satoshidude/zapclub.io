package main

import (
	"context"
	"database/sql"
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
	lastBeat int64 // ms of the last now_playing publish
	playing  bool
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

// stageEntry is the newest known state for one DJ in one club (from 30102 index).
type stageEntry struct {
	since    int64
	lastSeen int64
	on       bool
}

// premCacheEntry caches a per-club premium-owner check with a TTL so we avoid
// hitting the premium store on every tick.
type premCacheEntry struct {
	valid bool
	t     int64 // time.Now().UnixMilli() when cached
}

const premCacheTTL = 60_000 // ms

type conductor struct {
	db    *badger.BadgerBackend
	sq    *sql.DB // SQLite for persistent state; nil = disabled (graceful degradation)
	relay *khatru.Relay
	state *relay29.State // group membership/roles (skip authorization)
	sk    string
	pub   string
	mu    sync.Mutex
	clubs map[string]*condClub
	// played tracks per (club, dj, videoID); guarded by mu. Applied only to offline DJs so
	// their queue drains to the lobby instead of replaying infinitely when they can't sign.
	played map[string]map[string]map[string]bool

	presMu sync.Mutex
	pres   map[string]map[string]int64 // club → pubkey → last presence beat (ms)

	brokenMu    sync.Mutex
	broken      map[string]map[string]map[string]int64 // club → videoId → reporter → ts (ms)
	// Per-club throttle timestamps; guarded by mu.
	// Stored at conductor level (not inside condClub) so they survive condClub being
	// deleted and recreated by the tick cleanup loop.
	bootstrapAt  map[string]int64 // club → last bootstrap attempt ms
	brokenSkipAt map[string]int64 // club → last broken-skip ms

	// Event-driven in-memory indexes (populated by warmIndexes + observeEvent). Replacing
	// per-tick full-table DB scans with O(1) lookups. Guarded by idxMu.
	idxMu         sync.Mutex
	stageIdx      map[string]map[string]stageEntry  // club → pubkey → newest 30102
	kickIdx       map[string]map[string]int64        // club → dj → newest kick ms
	queueIdx      map[string]map[string]*nostr.Event // club → pubkey → newest 30103
	skipIdx       map[string]*nostr.Event            // club → newest 30107
	autoDJIdx     map[string]*nostr.Event            // club → newest 30105
	autoDJCtrlIdx map[string]nostr.Timestamp         // club → newest relay-signed 30111
	ownerCache    map[string]string                  // club → creator pubkey (permanent)
	premCache     map[string]premCacheEntry          // club → {valid bool, t ms}

	radioMgr *radioManager // set after construction; nil = no radio
}

func newConductor(db *badger.BadgerBackend, relay *khatru.Relay, state *relay29.State, sk string) *conductor {
	pub, _ := nostr.GetPublicKey(sk)
	return &conductor{
		db: db, relay: relay, state: state, sk: sk, pub: pub,
		clubs:         map[string]*condClub{},
		played:        map[string]map[string]map[string]bool{},
		pres:          map[string]map[string]int64{},
		broken:        map[string]map[string]map[string]int64{},
		bootstrapAt:  map[string]int64{},
		brokenSkipAt: map[string]int64{},
		stageIdx:      map[string]map[string]stageEntry{},
		kickIdx:       map[string]map[string]int64{},
		queueIdx:      map[string]map[string]*nostr.Event{},
		skipIdx:       map[string]*nostr.Event{},
		autoDJIdx:     map[string]*nostr.Event{},
		autoDJCtrlIdx: map[string]nostr.Timestamp{},
		ownerCache:    map[string]string{},
		premCache:     map[string]premCacheEntry{},
	}
}

// ── event-driven indexes ──────────────────────────────────────────────────────

// warmIndexes does one full scan per indexed kind at startup to seed the in-memory indexes.
// After this, observeEvent keeps them current via OnEventSaved.
func (c *conductor) warmIndexes(ctx context.Context) {
	for _, kind := range []int{kindStage, kindStageKick, kindQueue, kindSkip, kindAutoDJ, kindAutoDJCtrl} {
		ch, err := c.db.QueryEvents(ctx, nostr.Filter{Kinds: []int{kind}})
		if err != nil {
			log.Printf("conductor: warm index kind %d: %v", kind, err)
			continue
		}
		for ev := range ch {
			c.indexEvent(ev)
		}
	}
	log.Printf("conductor: indexes warmed")
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
	c.idxMu.Lock()
	m := c.queueIdx[club]
	if m == nil {
		m = map[string]*nostr.Event{}
		c.queueIdx[club] = m
	}
	if ex, ok := m[ev.PubKey]; !ok || ev.CreatedAt > ex.CreatedAt {
		m[ev.PubKey] = ev
	}
	c.idxMu.Unlock()
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

// isPremiumOwner returns whether the club's creator has an active premium subscription,
// using a short-TTL cache (premCacheTTL) to avoid hitting the premium store every tick.
func (c *conductor) isPremiumOwner(ctx context.Context, club string, now int64) bool {
	if condPrem == nil {
		return false
	}
	c.idxMu.Lock()
	if entry, ok := c.premCache[club]; ok && now-entry.t < premCacheTTL {
		c.idxMu.Unlock()
		return entry.valid
	}
	c.idxMu.Unlock()
	owner := c.clubOwner(ctx, club)
	if owner == "" {
		return false
	}
	valid := condPrem.valid(ctx, owner)
	c.idxMu.Lock()
	c.premCache[club] = premCacheEntry{valid: valid, t: now}
	c.idxMu.Unlock()
	return valid
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

func (c *conductor) sqClearState(club string) {
	if c.sq == nil {
		return
	}
	if _, err := c.sq.Exec(`DELETE FROM conductor_state WHERE club=?`, club); err != nil {
		log.Printf("sqlite clear_state [%.8s]: %v", club, err)
	}
	if _, err := c.sq.Exec(`DELETE FROM played WHERE club=?`, club); err != nil {
		log.Printf("sqlite clear_played [%.8s]: %v", club, err)
	}
}

func (c *conductor) sqRecordPlayed(club, dj, videoID string) {
	if c.sq == nil {
		return
	}
	if _, err := c.sq.Exec(`INSERT OR IGNORE INTO played(club,dj,video_id) VALUES(?,?,?)`, club, dj, videoID); err != nil {
		log.Printf("sqlite record_played [%.8s]: %v", club, err)
	}
}

// sqLoadState reads persisted conductor state for a club. Returns nil if none found.
func (c *conductor) sqLoadState(club string) *condClub {
	if c.sq == nil {
		return nil
	}
	row := c.sq.QueryRow(
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
func (c *conductor) sqLoadPlayed(club string) map[string]map[string]bool {
	if c.sq == nil {
		return nil
	}
	rows, err := c.sq.Query(`SELECT dj, video_id FROM played WHERE club=?`, club)
	if err != nil {
		return nil
	}
	defer rows.Close()
	out := map[string]map[string]bool{}
	for rows.Next() {
		var dj, vid string
		if err := rows.Scan(&dj, &vid); err != nil {
			continue
		}
		if out[dj] == nil {
			out[dj] = map[string]bool{}
		}
		out[dj][vid] = true
	}
	if len(out) == 0 {
		return nil
	}
	return out
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
	if c.sq == nil {
		return ""
	}
	var owner string
	if err := c.sq.QueryRow(`SELECT owner FROM club_owners WHERE club=?`, club).Scan(&owner); err != nil {
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
// selectActiveDjs: on + fresh (<1h) + not kicked. Reads from the in-memory indexes (no DB).
func (c *conductor) activeClubs(ctx context.Context) map[string][]condDJ {
	now := time.Now().UnixMilli()
	// Snapshot indexes under lock, then process without holding it.
	type djSnap struct {
		pubkey  string
		entry   stageEntry
		kickMs  int64 // newest kick for this dj in this club, or 0
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
		cap := condMaxDJsFree
		if c.isPremiumOwner(ctx, club, now) {
			cap = condMaxDJs
		}
		if len(list) > cap {
			list = list[:cap]
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
	for _, ev := range evs {
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
		// Notify the radio manager of the resumed track so streams restart immediately
		// after a relay restart (notifyRadio is otherwise only called by advance/stop).
		if pb.playing && pb.videoID != "" {
			c.notifyRadio(club, pb.videoID, pb.title)
		}
	}
	djPks := make([]string, len(djs))
	for i, d := range djs {
		djPks[i] = d.pubkey
	}

	// Not playing yet → bootstrap from the round-robin.
	// Throttled via a conductor-level map (survives condClub deletion/recreation by the
	// tick cleanup loop) to avoid busy-spinning when all queues are empty.
	if !pb.playing {
		if now-c.bootstrapAt[club] >= condHeartbeatMS {
			c.bootstrapAt[club] = now
			log.Printf("conductor [%.8s] advance reason=bootstrap pos=%d", club, pb.pos)
			c.advance(ctx, club, djPks, queues, pb, now)
		}
		return
	}
	// Orphan guard: the playing DJ left the stage → move on.
	if pb.dj != "" && indexOf(djPks, pb.dj) < 0 {
		log.Printf("conductor [%.8s] advance reason=orphan dj=%.8s", club, pb.dj)
		c.advance(ctx, club, djPks, queues, pb, now)
		return
	}
	// Takeover freeze: a staged DJ is live (kind 30109, mode=takeover, status=live).
	// Freeze the round-robin and publish a paused now_playing so clients stop the YT clock.
	// When the session ends/goes stale, resume with one advance() from the current position.
	if wasTakeover, resuming := c.takeoverState(ctx, club, djPks, pb, now); wasTakeover {
		if resuming {
			log.Printf("conductor [%.8s] advance reason=takeover-end", club)
			// Session just ended → clean resume from current round-robin position.
			c.advance(ctx, club, djPks, queues, pb, now)
		}
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
		c.stop(pb)
		c.sqClearState(club)
		c.notifyRadio(club, "", "")
		return
	}
	mat := c.matrix(djPks, queues, pb, club, now)
	di, ti := fairNext(djPks, pb.dj, mat)
	if di == -1 {
		// Every DJ's queue is fully played-off (or empty) → stop to the lobby. No auto-loop: a set
		// plays through once, then the lobby runs until a DJ adds / re-activates tracks.
		log.Printf("conductor [%.8s] stop: no playable track djs=%d", club, len(djPks))
		c.stop(pb)
		delete(c.played, club) // clear in-memory played-set — session boundary
		c.sqClearState(club)   // clear SQLite state + played rows
		c.notifyRadio(club, "", "")
		return
	}
	tracks := queues[djPks[di]]
	if ti >= len(tracks) {
		log.Printf("conductor [%.8s] stop: track index out of range di=%d ti=%d", club, di, ti)
		c.stop(pb)
		c.sqClearState(club)
		c.notifyRadio(club, "", "")
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
	// Record into the relay played-set. Applied only to offline DJs (see matrix()) so their
	// queue drains to lobby when they can't sign. Online DJs are unaffected.
	if c.played[club] == nil {
		c.played[club] = map[string]map[string]bool{}
	}
	if c.played[club][pb.dj] == nil {
		c.played[club][pb.dj] = map[string]bool{}
	}
	c.played[club][pb.dj][t.videoID] = true
	c.sqRecordPlayed(club, pb.dj, t.videoID)
	c.publishNowPlaying(ctx, club, pb, now)
	c.publishPlay(ctx, club, pb, now)
	c.sqSaveState(club, pb)
	c.notifyRadio(club, t.videoID, t.title)
}

// matrix builds the playability matrix. A track is playable if it is `active` (NOT marked `off`)
// and not the currently-playing one. For OFFLINE DJs, tracks recorded in the relay played-set
// (from 1313 play-log + in-memory advance() records) are additionally excluded so their queue
// drains to lobby instead of replaying forever when they can't sign. Online DJs are unaffected
// (their browser marks tracks off via the visible queue — the single source of truth for them).
func (c *conductor) matrix(djPks []string, queues map[string][]condTrack, pb *condClub, club string, now int64) [][]bool {
	out := make([][]bool, len(djPks))
	for i, pk := range djPks {
		tracks := queues[pk]
		row := make([]bool, len(tracks))
		djOnline := c.online(club, pk, now)
		playedByDJ := c.played[club][pk] // nil-safe: map lookup on nil map is zero value
		for j, t := range tracks {
			playedOff := !djOnline && playedByDJ[t.videoID]
			row[j] = t.active && t.videoID != pb.videoID && !playedOff
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

// currentVideoID returns the currently playing video for a club, or "" if stopped/lobby.
func (c *conductor) currentVideoID(clubID string) string {
	c.mu.Lock()
	defer c.mu.Unlock()
	pb := c.clubs[clubID]
	if pb == nil || !pb.playing {
		return ""
	}
	return pb.videoID
}

// notifyRadio forwards track changes to the radio manager (no-op if radio is not set up).
func (c *conductor) notifyRadio(clubID, videoID, title string) {
	if c.radioMgr != nil {
		c.radioMgr.onTrackChange(clubID, videoID, title)
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
		log.Printf("conductor [%.8s] resume: sqlite pos=%d playing=%v", club, sq.pos, sq.playing)
		if played := c.sqLoadPlayed(club); played != nil {
			c.played[club] = played
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
				c.played[club] = map[string]map[string]bool{}
			}
			if c.played[club][dj] == nil {
				c.played[club][dj] = map[string]bool{}
			}
			c.played[club][dj][vid] = true
		}
	}
	// Persist to SQLite so the next restart uses the fast path.
	c.sqSaveState(club, pb)
	for dj, vids := range c.played[club] {
		for vid := range vids {
			c.sqRecordPlayed(club, dj, vid)
		}
	}
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
// Reads from the in-memory indexes (no DB).
func (c *conductor) armedAutoClubs(ctx context.Context, active map[string][]condDJ) map[string]*autoState {
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

	if c.skipRequested(ctx, club, pb) {
		nextTrack()
		startTrack()
		return
	}
	if c.brokenSkip(club, pb, now) && now-c.brokenSkipAt[club] >= condHeartbeatMS {
		c.brokenSkipAt[club] = now
		log.Printf("conductor [%.8s] advance reason=broken-auto vid=%s", club, pb.videoID)
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
	prem       *premiumStore
	superadmin string
	// Set after the conductor is initialized (main.go) so reject() reads
	// the conductor's in-memory indexes instead of querying BadgerDB.
	countFn      func(club, sender string) (active int, alreadyOnStage bool)
	isPremOwnerFn func(ctx context.Context, club string, now int64) bool
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

	cap := condMaxDJsFree
	if g.isPremOwnerFn != nil {
		if g.isPremOwnerFn(ctx, club, time.Now().UnixMilli()) {
			cap = condMaxDJs
		}
	} else if owner := clubOwnerFromDB(ctx, g.db, club); owner != "" {
		if (g.superadmin != "" && owner == g.superadmin) || (g.prem != nil && g.prem.valid(ctx, owner)) {
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

// isOnStage reports whether pk currently holds an active stage slot in club.
// Reads from the in-memory indexes (no DB). Used by the RTMP HTTP handler.
func (c *conductor) isOnStage(club, pk string) bool {
	now := time.Now().UnixMilli()
	c.idxMu.Lock()
	entry, ok := c.stageIdx[club][pk]
	kickMs := c.kickIdx[club][pk]
	c.idxMu.Unlock()
	if !ok || !entry.on {
		return false
	}
	if entry.lastSeen < now-condStageStaleMS {
		return false
	}
	if kickMs > 0 && entry.lastSeen <= kickMs {
		return false
	}
	return true
}

// currentTrack returns the currently playing videoID and the elapsed seconds for club.
// Returns ("", 0) when nothing is playing. Used by the RTMP HTTP handler.
func (c *conductor) currentTrack(club string) (videoID string, seekSec int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	pb := c.clubs[club]
	if pb == nil || !pb.playing || pb.videoID == "" {
		return "", 0
	}
	elapsed := (time.Now().UnixMilli() - pb.startedAt) / 1000
	return pb.videoID, int(elapsed)
}
