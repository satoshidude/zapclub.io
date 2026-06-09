package main

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"sort"
	"sync"

	"github.com/nbd-wtf/go-nostr"
)

// Global all-time zap leaderboard. Built from kind-20101 club zap broadcasts — the same soft,
// self-reported signal that drives the in-room zap score. A NIP-57 9735 receipt is the hard
// proof of a zap, but many LNURL providers (e.g. nsnip.io) never publish one, so the broadcast
// is what every client actually sees; the leaderboard is therefore at the SAME trust level as
// the live score (membership-gated, but not a cryptographic proof — a determined member could
// inflate it). Self-zaps (sender == recipient) are dropped so nobody ranks themselves up, and
// each zap is counted once (bolt11, else event id). Per recipient we keep total sats, zap count
// and the set of distinct senders (→ "from N people"). Persisted to disk so the board survives
// restarts. Served PUBLIC and unauthenticated at GET /leaderboard (it's public ranking data).

const (
	kindZapBroadcast = 20101
	lbMaxRecipients  = 100_000 // memory-DoS cap on tracked recipients
	lbMaxSenders     = 5_000   // cap distinct senders tracked per recipient
	lbSeenCap        = 500_000 // bounded in-memory dedup of counted zaps
	lbTopN           = 100     // entries returned by the public (no-pubkey) board
)

type zapEntry struct {
	Sats    int64           `json:"sats"`
	Zaps    int             `json:"zaps"`
	Senders map[string]bool `json:"senders"` // distinct sender pubkeys → count = "zappers"
}

type zapBoard struct {
	mu   sync.Mutex
	path string
	By   map[string]*zapEntry `json:"by"`   // recipient pubkey → totals (persisted)
	seen map[string]bool      // dedup key → counted (in-memory only; ephemeral source)
}

func newZapBoard(path string) *zapBoard {
	b := &zapBoard{path: path, By: map[string]*zapEntry{}, seen: map[string]bool{}}
	if data, err := os.ReadFile(path); err == nil {
		_ = json.Unmarshal(data, b) // best-effort
	}
	if b.By == nil {
		b.By = map[string]*zapEntry{}
	}
	b.seen = map[string]bool{}
	return b
}

// observe records an accepted kind-20101 zap broadcast. Registered on OnEphemeralEvent, so it
// only sees broadcasts that already passed the relay's membership write-protection.
func (b *zapBoard) observe(_ context.Context, ev *nostr.Event) {
	if ev.Kind != kindZapBroadcast {
		return
	}
	recipient := tagVal(ev, "p")
	if recipient == "" || recipient == ev.PubKey { // no self-zap onto the board
		return
	}
	sats := int64(atoiDefault(tagVal(ev, "amount"), 0))
	if sats <= 0 {
		return
	}
	dk := tagVal(ev, "bolt11")
	if dk == "" {
		dk = "id:" + ev.ID
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.seen[dk] {
		return // same zap already counted
	}
	if len(b.seen) >= lbSeenCap {
		b.seen = map[string]bool{} // bounded; rare reset (worst case: a few re-counts)
	}
	b.seen[dk] = true
	e := b.By[recipient]
	if e == nil {
		if len(b.By) >= lbMaxRecipients {
			return
		}
		e = &zapEntry{Senders: map[string]bool{}}
		b.By[recipient] = e
	}
	if e.Senders == nil {
		e.Senders = map[string]bool{}
	}
	e.Sats += sats
	e.Zaps++
	if !e.Senders[ev.PubKey] && len(e.Senders) < lbMaxSenders {
		e.Senders[ev.PubKey] = true
	}
}

// ── snapshots ────────────────────────────────────────────────────────────────

type lbEntry struct {
	Pubkey  string `json:"pubkey"`
	Sats    int64  `json:"sats"`
	Zaps    int    `json:"zaps"`
	Zappers int    `json:"zappers"`
	Rank    int    `json:"rank"`
}

// rankOf returns one recipient's entry incl. global rank (competition ranking: 1 + the number
// of recipients with strictly more sats), the total participant count, and whether they're on
// the board at all.
func (b *zapBoard) rankOf(pubkey string) (entry lbEntry, total int, ok bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	total = len(b.By)
	e := b.By[pubkey]
	if e == nil {
		return lbEntry{}, total, false
	}
	rank := 1
	for pk, other := range b.By {
		if pk != pubkey && other.Sats > e.Sats {
			rank++
		}
	}
	return lbEntry{Pubkey: pubkey, Sats: e.Sats, Zaps: e.Zaps, Zappers: len(e.Senders), Rank: rank}, total, true
}

// top returns the n highest recipients (sats desc, pubkey tiebreak), each with an ordinal rank,
// plus the total participant count.
func (b *zapBoard) top(n int) (entries []lbEntry, total int) {
	b.mu.Lock()
	all := make([]lbEntry, 0, len(b.By))
	for pk, e := range b.By {
		all = append(all, lbEntry{Pubkey: pk, Sats: e.Sats, Zaps: e.Zaps, Zappers: len(e.Senders)})
	}
	total = len(b.By)
	b.mu.Unlock()
	sort.Slice(all, func(i, j int) bool {
		if all[i].Sats != all[j].Sats {
			return all[i].Sats > all[j].Sats
		}
		return all[i].Pubkey < all[j].Pubkey
	})
	for i := range all {
		all[i].Rank = i + 1
	}
	if len(all) > n {
		all = all[:n]
	}
	return all, total
}

// handleHTTP serves the public leaderboard. ?pubkey=<hex> → that user's rank + totals;
// otherwise the top N. No auth — public ranking data; CORS open for read-only use.
func (b *zapBoard) handleHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Vary", "Origin")
	if r.Method == http.MethodOptions {
		w.Header().Set("Access-Control-Allow-Methods", "GET, OPTIONS")
		w.WriteHeader(http.StatusNoContent)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "public, max-age=30")
	enc := json.NewEncoder(w)
	if pk := r.URL.Query().Get("pubkey"); pk != "" {
		e, total, ok := b.rankOf(pk)
		if !ok {
			_ = enc.Encode(map[string]any{"ranked": false, "total": total})
			return
		}
		_ = enc.Encode(map[string]any{
			"ranked": true, "total": total, "pubkey": e.Pubkey,
			"sats": e.Sats, "zaps": e.Zaps, "zappers": e.Zappers, "rank": e.Rank,
		})
		return
	}
	entries, total := b.top(lbTopN)
	_ = enc.Encode(map[string]any{"total": total, "top": entries})
}

func (b *zapBoard) save() {
	b.mu.Lock()
	data, err := json.Marshal(b)
	b.mu.Unlock()
	if err != nil {
		return
	}
	tmp := b.path + ".tmp"
	if os.WriteFile(tmp, data, 0o600) != nil {
		return
	}
	_ = os.Rename(tmp, b.path)
}
