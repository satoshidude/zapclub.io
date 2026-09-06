package main

import (
	"context"
	"encoding/json"
	"log"
	"os"
	"sort"
	"sync"
	"time"

	"github.com/fiatjaf/khatru"
	"github.com/nbd-wtf/go-nostr"
)

// Listener analytics for the superadmin dashboard. Anonymous club-page heartbeats (kind
// 20105) are ephemeral — khatru broadcasts but never stores them — so we observe them via
// OnEphemeralEvent and keep our OWN rolling 24h record: per club, a per-5-min count series
// (for the chart) plus, per anonymous browser-tab session, the first/last time it was seen.
// Login keys are deliberately not used: open club pages count equally for guests, read-only
// accounts and signed-in members. Persisted to disk so the 24h window survives deploys.
const (
	kindPresence           = 20100 // signed-in member presence; separate from listener sessions
	kindListenerBeat       = 20105
	kindListenerCount      = 20106               // relay-signed aggregate; clients cannot forge it
	listenWindowMs         = 24 * 60 * 60 * 1000 // rolling window: 24h
	listenBucketMs         = 5 * 60 * 1000       // chart resolution: 5 min
	listenOnlineMs         = 70 * 1000           // browser tabs beat about every 25s
	listenCountBroadcastMs = 15 * 1000           // refresh stable counts for late subscribers
	listenMaxClubs         = 2000                // safety caps against unbounded growth
	listenMaxPksClb        = 5000
)

// listenerSample is one finalized 5-min bucket: T = bucket start (ms), N = distinct
// listeners seen during it.
type listenerSample struct {
	T int64 `json:"t"`
	N int   `json:"n"`
}

// span is the first/last time a pubkey was seen in a club within the window.
type span struct {
	First int64 `json:"first"`
	Last  int64 `json:"last"`
}

type listenerStats struct {
	mu            sync.Mutex
	persistMu     sync.Mutex
	path          string
	Seen          map[string]map[string]*span    `json:"seen"`     // club -> anonymous session pubkey -> span
	Series        map[string][]listenerSample    `json:"series"`   // club -> finalized buckets
	CurStart      int64                          `json:"curStart"` // start (ms) of the open bucket
	CurSets       map[string]map[string]struct{} `json:"-"`        // club -> distinct sessions this bucket
	active        map[string]map[string]int64    `json:"-"`        // club -> session -> last beat (ms)
	published     map[string]int                 `json:"-"`        // last aggregate sent per club
	lastPublished map[string]int64               `json:"-"`
	publishCount  func(club string, count int, now int64)
}

func newListenerStats(path string) *listenerStats {
	s := &listenerStats{
		path:          path,
		Seen:          map[string]map[string]*span{},
		Series:        map[string][]listenerSample{},
		CurSets:       map[string]map[string]struct{}{},
		active:        map[string]map[string]int64{},
		published:     map[string]int{},
		lastPublished: map[string]int64{},
	}
	if data, err := os.ReadFile(path); err == nil {
		_ = json.Unmarshal(data, s) // best-effort; CurSets is rebuilt empty (in-flight bucket only)
		if s.Seen == nil {
			s.Seen = map[string]map[string]*span{}
		}
		if s.Series == nil {
			s.Series = map[string][]listenerSample{}
		}
		s.CurSets = map[string]map[string]struct{}{}
	}
	return s
}

func (s *listenerStats) setCountPublisher(fn func(club string, count int, now int64)) {
	s.mu.Lock()
	s.publishCount = fn
	s.mu.Unlock()
}

func validListenerBeat(event *nostr.Event, now nostr.Timestamp) bool {
	if event == nil || event.Kind != kindListenerBeat || event.Content != "" {
		return false
	}
	// Old captured heartbeats must not be replayable as fresh listeners. Future timestamps are
	// rejected by the relay's shared policy; this enforces the matching lower bound.
	if event.CreatedAt < now-60 {
		return false
	}
	var h, state int
	for _, tag := range event.Tags {
		if len(tag) != 2 {
			return false
		}
		switch tag[0] {
		case "h":
			h++
			if tag[1] == "" {
				return false
			}
		case "state":
			state++
			if tag[1] != "on" && tag[1] != "off" {
				return false
			}
		default:
			return false
		}
	}
	return h == 1 && state == 1
}

// Raw session heartbeats are input-only. Even though their keys are anonymous and ephemeral,
// other clients need only the relay-signed aggregate and must not receive the individual beats.
func preventListenerBeatBroadcast(_ *khatru.WebSocket, event *nostr.Event) bool {
	return event.Kind == kindListenerBeat
}

// observe records an accepted anonymous club-page heartbeat. "off" is a best-effort fast
// departure signal; missing departures age out through listenOnlineMs.
func (s *listenerStats) observe(_ context.Context, evt *nostr.Event) {
	if evt.Kind != kindListenerBeat {
		return
	}
	var club, state string
	for _, t := range evt.Tags {
		if len(t) >= 2 && t[0] == "h" {
			club = t[1]
		}
		if len(t) >= 2 && t[0] == "state" {
			state = t[1]
		}
	}
	if club == "" {
		return
	}
	now := time.Now().UnixMilli()
	if state == "off" {
		s.remove(club, evt.PubKey)
	} else {
		s.record(club, evt.PubKey, now)
	}
	s.broadcastLive(now)
}

func (s *listenerStats) record(club, pubkey string, now int64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.rollLocked(now)

	active := s.active[club]
	if active == nil {
		if len(s.active) >= listenMaxClubs {
			return
		}
		active = map[string]int64{}
		s.active[club] = active
	}
	if _, exists := active[pubkey]; !exists && len(active) >= listenMaxPksClb {
		return
	}
	active[pubkey] = now

	set := s.CurSets[club]
	if set == nil {
		if len(s.CurSets) >= listenMaxClubs {
			return
		}
		set = map[string]struct{}{}
		s.CurSets[club] = set
	}
	if _, exists := set[pubkey]; !exists && len(set) >= listenMaxPksClb {
		return
	}
	set[pubkey] = struct{}{}

	cs := s.Seen[club]
	if cs == nil {
		cs = map[string]*span{}
		s.Seen[club] = cs
	}
	if sp := cs[pubkey]; sp != nil {
		sp.Last = now
	} else if len(cs) < listenMaxPksClb {
		cs[pubkey] = &span{First: now, Last: now}
	}
}

func (s *listenerStats) remove(club, pubkey string) {
	s.mu.Lock()
	if active := s.active[club]; active != nil {
		delete(active, pubkey)
		if len(active) == 0 {
			delete(s.active, club)
		}
	}
	s.mu.Unlock()
}

// deleteClub removes both live and retained anonymous analytics for an administratively deleted
// club. In particular, clearing published prevents the periodic broadcaster from recreating a
// relay-signed zero-count event after the Badger purge.
func (s *listenerStats) deleteClub(club string) error {
	if club == "" {
		return nil
	}
	s.mu.Lock()
	delete(s.Seen, club)
	delete(s.Series, club)
	delete(s.CurSets, club)
	delete(s.active, club)
	delete(s.published, club)
	delete(s.lastPublished, club)
	s.mu.Unlock()
	return s.save()
}

func (s *listenerStats) pruneActiveLocked(now int64) {
	for club, sessions := range s.active {
		for pubkey, last := range sessions {
			if now-last >= listenOnlineMs {
				delete(sessions, pubkey)
			}
		}
		if len(sessions) == 0 {
			delete(s.active, club)
		}
	}
}

// broadcastLive publishes relay-authoritative aggregate counts when they change and at a
// low refresh cadence so a newly connected page never waits for another listener to arrive.
func (s *listenerStats) broadcastLive(now int64) {
	type update struct {
		club  string
		count int
	}

	s.mu.Lock()
	s.pruneActiveLocked(now)
	if s.publishCount == nil {
		s.mu.Unlock()
		return
	}
	clubs := make(map[string]struct{}, len(s.active)+len(s.published))
	for club := range s.active {
		clubs[club] = struct{}{}
	}
	for club := range s.published {
		clubs[club] = struct{}{}
	}
	updates := make([]update, 0, len(clubs))
	for club := range clubs {
		count := len(s.active[club])
		previous, sent := s.published[club]
		if !sent || previous != count || now-s.lastPublished[club] >= listenCountBroadcastMs {
			updates = append(updates, update{club: club, count: count})
			s.published[club] = count
			s.lastPublished[club] = now
		}
		if count == 0 {
			delete(s.published, club)
			delete(s.lastPublished, club)
		}
	}
	publish := s.publishCount
	s.mu.Unlock()

	for _, u := range updates {
		publish(u.club, u.count, now)
	}
}

// rollLocked finalizes elapsed buckets into Series and advances the open bucket to `now`,
// filling continuous (incl. zero) buckets for every club that had a listener in the window.
func (s *listenerStats) rollLocked(now int64) {
	const bucket = int64(listenBucketMs)
	if s.CurStart == 0 {
		s.CurStart = now - now%bucket
		return
	}
	for now >= s.CurStart+bucket {
		for club := range s.clubUniverseLocked() {
			s.Series[club] = append(s.Series[club], listenerSample{T: s.CurStart, N: len(s.CurSets[club])})
		}
		s.CurStart += bucket
		s.CurSets = map[string]map[string]struct{}{}
	}
	s.trimLocked(now)
}

// clubUniverseLocked = clubs with a listener in the window (Seen) or in the open bucket.
func (s *listenerStats) clubUniverseLocked() map[string]struct{} {
	u := make(map[string]struct{}, len(s.Seen)+len(s.CurSets))
	for c := range s.Seen {
		u[c] = struct{}{}
	}
	for c := range s.CurSets {
		u[c] = struct{}{}
	}
	return u
}

// trimLocked drops samples and spans older than the window; empties are removed so idle
// clubs eventually leave the tracker entirely.
func (s *listenerStats) trimLocked(now int64) {
	s.pruneActiveLocked(now)
	cutoff := now - listenWindowMs
	for club, samples := range s.Series {
		kept := samples[:0]
		for _, x := range samples {
			if x.T >= cutoff {
				kept = append(kept, x)
			}
		}
		if len(kept) == 0 {
			delete(s.Series, club)
		} else {
			s.Series[club] = kept
		}
	}
	for club, pks := range s.Seen {
		for pk, sp := range pks {
			if sp.Last < cutoff {
				delete(pks, pk)
			}
		}
		if len(pks) == 0 {
			delete(s.Seen, club)
		}
	}
}

// ── snapshot for the admin endpoint ──────────────────────────────────────────

type seenListener struct {
	Pubkey string `json:"pubkey"`
	First  int64  `json:"first"`
	Last   int64  `json:"last"`
}

type clubListeners struct {
	ID     string           `json:"id"`
	Live   []string         `json:"live"`   // pubkeys beating right now
	Series []listenerSample `json:"series"` // 24h count buckets (incl. the open one)
	Seen   []seenListener   `json:"seen"`   // who listened in the window + their span
}

type listenersResp struct {
	GeneratedAt int64           `json:"generatedAt"`
	BucketMs    int64           `json:"bucketMs"`
	WindowMs    int64           `json:"windowMs"`
	Clubs       []clubListeners `json:"clubs"`
}

func (s *listenerStats) snapshot(now int64) listenersResp {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.rollLocked(now)

	// Initialise slices non-nil so JSON encodes [] (not null) for an idle relay — the
	// client treats null as a hard error otherwise.
	resp := listenersResp{GeneratedAt: now, BucketMs: listenBucketMs, WindowMs: listenWindowMs, Clubs: []clubListeners{}}
	for club := range s.clubUniverseLocked() {
		cl := clubListeners{ID: club, Live: []string{}, Seen: []seenListener{}, Series: []listenerSample{}}
		// finalized buckets + the still-open bucket as the live tail
		cl.Series = append(cl.Series, s.Series[club]...)
		cl.Series = append(cl.Series, listenerSample{T: s.CurStart, N: len(s.CurSets[club])})
		for pk := range s.active[club] {
			cl.Live = append(cl.Live, pk)
		}
		for pk, sp := range s.Seen[club] {
			cl.Seen = append(cl.Seen, seenListener{Pubkey: pk, First: sp.First, Last: sp.Last})
		}
		sort.Slice(cl.Seen, func(i, j int) bool { return cl.Seen[i].Last > cl.Seen[j].Last })
		sort.Strings(cl.Live)
		resp.Clubs = append(resp.Clubs, cl)
	}
	sort.Slice(resp.Clubs, func(i, j int) bool {
		if len(resp.Clubs[i].Live) != len(resp.Clubs[j].Live) {
			return len(resp.Clubs[i].Live) > len(resp.Clubs[j].Live)
		}
		return resp.Clubs[i].ID < resp.Clubs[j].ID
	})
	return resp
}

// tick advances buckets + trims even when no beats arrive (so the live count expires and
// the timeline stays continuous), then persists. Called periodically from main.
func (s *listenerStats) tick(now int64, persist bool) {
	s.mu.Lock()
	s.rollLocked(now)
	s.mu.Unlock()
	if persist {
		s.save()
	}
}

func (s *listenerStats) save() error {
	// Serialize the snapshot and the fixed-name atomic replace as one operation. Otherwise a
	// periodic save racing an administrative deletion could restore an older snapshot last.
	s.persistMu.Lock()
	defer s.persistMu.Unlock()
	s.mu.Lock()
	data, err := json.Marshal(s)
	s.mu.Unlock()
	if err != nil {
		return err
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		log.Printf("listener analytics save: %v", err)
		return err
	}
	if err := os.Rename(tmp, s.path); err != nil {
		log.Printf("listener analytics rename: %v", err)
		return err
	}
	return nil
}
