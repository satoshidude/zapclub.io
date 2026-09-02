package main

import (
	"context"
	"encoding/json"
	"os"
	"sort"
	"sync"
	"time"

	"github.com/nbd-wtf/go-nostr"
)

// Listener analytics for the superadmin dashboard. Presence heartbeats (kind 20100) are
// ephemeral — khatru broadcasts but never stores them — so we observe them via
// OnEphemeralEvent and keep our OWN rolling 24h record: per club, a per-5-min count series
// (for the chart) plus, per listener, the first/last time they were seen (who listened,
// when). This is a deliberate, superadmin-only analytics store — the only place zapclub
// records who-listened-when. It holds NOTHING about non-members (the relay rejects their
// writes, so they never beat) and nothing about what was played. Persisted to disk so the
// 24h window survives relay restarts/deploys.
const (
	kindPresence    = 20100
	listenWindowMs  = 24 * 60 * 60 * 1000 // rolling window: 24h
	listenBucketMs  = 5 * 60 * 1000       // chart resolution: 5 min
	listenOnlineMs  = 60 * 1000           // a beat keeps a pubkey "live" for 60s (beats are ~25s)
	listenMaxClubs  = 2000                // safety caps against unbounded growth
	listenMaxPksClb = 5000
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
	mu       sync.Mutex
	path     string
	Seen     map[string]map[string]*span    `json:"seen"`     // club -> pubkey -> span
	Series   map[string][]listenerSample    `json:"series"`   // club -> finalized buckets
	CurStart int64                          `json:"curStart"` // start (ms) of the open bucket
	CurSets  map[string]map[string]struct{} `json:"-"`        // club -> distinct pubkeys this bucket
}

func newListenerStats(path string) *listenerStats {
	s := &listenerStats{
		path:    path,
		Seen:    map[string]map[string]*span{},
		Series:  map[string][]listenerSample{},
		CurSets: map[string]map[string]struct{}{},
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

// observe records an accepted presence heartbeat. Registered on OnEphemeralEvent, so it
// only ever sees member beats that already passed the write-protection checks.
func (s *listenerStats) observe(_ context.Context, evt *nostr.Event) {
	if evt.Kind != kindPresence {
		return
	}
	var club string
	for _, t := range evt.Tags {
		if len(t) >= 2 && t[0] == "h" {
			club = t[1]
			break
		}
	}
	if club == "" {
		return
	}
	s.record(club, evt.PubKey, time.Now().UnixMilli())
}

func (s *listenerStats) record(club, pubkey string, now int64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.rollLocked(now)

	set := s.CurSets[club]
	if set == nil {
		if len(s.CurSets) >= listenMaxClubs {
			return
		}
		set = map[string]struct{}{}
		s.CurSets[club] = set
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
		for pk, sp := range s.Seen[club] {
			if now-sp.Last < listenOnlineMs {
				cl.Live = append(cl.Live, pk)
			}
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

func (s *listenerStats) save() {
	s.mu.Lock()
	data, err := json.Marshal(s)
	s.mu.Unlock()
	if err != nil {
		return
	}
	tmp := s.path + ".tmp"
	if os.WriteFile(tmp, data, 0o600) != nil {
		return
	}
	_ = os.Rename(tmp, s.path)
}
