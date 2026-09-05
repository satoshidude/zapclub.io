package main

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/nbd-wtf/go-nostr"
)

// Global all-time Zapclub leaderboard. Club zaps arrive as kind-20101 broadcasts; direct profile
// zaps arrive through the authenticated HTTP endpoint below. Both are the same soft, self-reported
// payment signal: many LNURL providers never publish a NIP-57 receipt, so the client reports a
// confirmed invoice. Self-zaps are dropped, each invoice is counted once, and the recipient comes
// from the explicit target rather than a wallet provider's global receipt history. Public totals
// are served at GET /leaderboard; the sender breakdown is only returned to its NIP-98-authenticated
// recipient at GET /zaps/received.

const (
	kindZapBroadcast = 20101
	anonZapSender    = "__anon__"
	lbMaxRecipients  = 100_000 // memory-DoS cap on tracked recipients
	lbMaxSenders     = 5_000   // cap distinct senders tracked per recipient
	lbSeenCap        = 500_000 // bounded in-memory dedup of counted zaps
	lbTopN           = 100     // entries returned by the public (no-pubkey) board
)

type zapEntry struct {
	Sats     int64                      `json:"sats"`
	Zaps     int                        `json:"zaps"`
	Senders  map[string]bool            `json:"senders"` // retained for backwards compatibility
	BySender map[string]*zapSenderEntry `json:"by_sender,omitempty"`
	Legacy   bool                       `json:"legacy,omitempty"` // old aggregate lacks per-sender amounts
}

type zapSenderEntry struct {
	Sats int64 `json:"sats"`
	Zaps int   `json:"zaps"`
}

type zapBoard struct {
	mu   sync.Mutex
	path string
	By   map[string]*zapEntry `json:"by"` // recipient pubkey → totals (persisted)
	seen map[string]bool      // dedup key → counted (in-memory only; ephemeral source)
	pub  *kindLimiter         // report budget per signed request author
	ip   *ipLimiter           // report budget per HTTP client
}

func newZapBoard(path string) *zapBoard {
	b := &zapBoard{
		path: path, By: map[string]*zapEntry{}, seen: map[string]bool{},
		pub: newKindLimiter(6, 0.1, "rate-limited: too many zap reports", nostr.KindZapRequest),
		ip:  newIPLimiter(30, 1),
	}
	if data, err := os.ReadFile(path); err == nil {
		_ = json.Unmarshal(data, b) // best-effort
	}
	if b.By == nil {
		b.By = map[string]*zapEntry{}
	}
	for _, entry := range b.By {
		if entry.Senders == nil {
			entry.Senders = map[string]bool{}
		}
		if entry.BySender == nil {
			entry.BySender = map[string]*zapSenderEntry{}
			if entry.Zaps > 0 {
				entry.Legacy = true
			}
		}
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
	sats := int64(atoiDefault(tagVal(ev, "amount"), 0))
	dk := tagVal(ev, "bolt11")
	if dk == "" {
		dk = "id:" + ev.ID
	} else {
		dk = "bolt11:" + strings.ToLower(dk)
	}
	b.record(ev.PubKey, tagVal(ev, "p"), sats, dk)
}

func (b *zapBoard) record(sender, recipient string, sats int64, dedupKey string) {
	if sender == "" || recipient == "" || sender == recipient || sats <= 0 || dedupKey == "" {
		return
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.seen[dedupKey] {
		return // same zap already counted
	}
	if len(b.seen) >= lbSeenCap {
		b.seen = map[string]bool{} // bounded; rare reset (worst case: a few re-counts)
	}
	b.seen[dedupKey] = true
	e := b.By[recipient]
	if e == nil {
		if len(b.By) >= lbMaxRecipients {
			return
		}
		e = &zapEntry{Senders: map[string]bool{}, BySender: map[string]*zapSenderEntry{}}
		b.By[recipient] = e
	}
	if e.Senders == nil {
		e.Senders = map[string]bool{}
	}
	if e.BySender == nil {
		e.BySender = map[string]*zapSenderEntry{}
	}
	e.Sats += sats
	e.Zaps++
	if !e.Senders[sender] && len(e.Senders) < lbMaxSenders {
		e.Senders[sender] = true
	}
	if senderEntry := e.BySender[sender]; senderEntry != nil {
		senderEntry.Sats += sats
		senderEntry.Zaps++
	} else if len(e.BySender) < lbMaxSenders {
		e.BySender[sender] = &zapSenderEntry{Sats: sats, Zaps: 1}
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

type receivedSender struct {
	Sender string `json:"sender"`
	Sats   int64  `json:"sats,omitempty"`
	Count  int    `json:"count,omitempty"`
	Exact  bool   `json:"exact"`
	Anon   bool   `json:"anon"`
}

type receivedZaps struct {
	Total    int64            `json:"total"`
	Count    int              `json:"count"`
	BySender []receivedSender `json:"bySender"`
}

func (b *zapBoard) received(pubkey string) receivedZaps {
	b.mu.Lock()
	defer b.mu.Unlock()
	result := receivedZaps{BySender: []receivedSender{}}
	e := b.By[pubkey]
	if e == nil {
		return result
	}
	result.Total, result.Count = e.Sats, e.Zaps
	for sender := range e.Senders {
		row := receivedSender{Sender: sender, Exact: !e.Legacy, Anon: sender == anonZapSender}
		if stats := e.BySender[sender]; stats != nil && !e.Legacy {
			row.Sats, row.Count = stats.Sats, stats.Zaps
		}
		result.BySender = append(result.BySender, row)
	}
	sort.Slice(result.BySender, func(i, j int) bool {
		if result.BySender[i].Sats != result.BySender[j].Sats {
			return result.BySender[i].Sats > result.BySender[j].Sats
		}
		return result.BySender[i].Sender < result.BySender[j].Sender
	})
	return result
}

// handleZaps records site-confirmed zaps from their signed NIP-57 requests and serves a
// recipient's private sender breakdown. The request signature binds sender, recipient and amount;
// the client marker excludes zap requests created outside Zapclub.
func (b *zapBoard) handleZaps(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Access-Control-Allow-Origin", allowOrigin)
	w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type")
	w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
	w.Header().Set("Vary", "Origin")
	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	switch {
	case r.URL.Path == "/zaps/received" && r.Method == http.MethodGet:
		pubkey, ok := verifyNIP98(r)
		if !ok {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		_ = json.NewEncoder(w).Encode(b.received(pubkey))
	case r.URL.Path == "/zaps" && r.Method == http.MethodPost:
		if !b.ip.allow(clientIP(r)) {
			http.Error(w, "rate limited", http.StatusTooManyRequests)
			return
		}
		var body struct {
			Request nostr.Event `json:"request"`
			Invoice string      `json:"invoice"`
		}
		dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096))
		if err := dec.Decode(&body); err != nil || body.Request.Kind != nostr.KindZapRequest ||
			!hasTagValue(body.Request.Tags, "client", "zapclub.io") ||
			len(body.Invoice) < 8 || len(body.Invoice) > 4096 ||
			!strings.HasPrefix(strings.ToLower(body.Invoice), "ln") {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		if ok, err := body.Request.CheckSignature(); !ok || err != nil {
			http.Error(w, "invalid zap request", http.StatusBadRequest)
			return
		}
		if reject, reason := b.pub.reject(r.Context(), &body.Request); reject {
			http.Error(w, reason, http.StatusTooManyRequests)
			return
		}
		recipient := tagVal(&body.Request, "p")
		msats := int64(atoiDefault(tagVal(&body.Request, "amount"), 0))
		if !nostr.IsValidPublicKey(recipient) || msats < 1000 {
			http.Error(w, "invalid zap target", http.StatusBadRequest)
			return
		}
		sender := body.Request.PubKey
		if sender == recipient {
			_ = json.NewEncoder(w).Encode(map[string]bool{"ok": true})
			return
		}
		if hasTagValue(body.Request.Tags, "anon", "") {
			sender = anonZapSender
		}
		b.record(sender, recipient, msats/1000, "bolt11:"+strings.ToLower(body.Invoice))
		_ = json.NewEncoder(w).Encode(map[string]bool{"ok": true})
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (b *zapBoard) sweep() {
	b.pub.sweep(10 * time.Minute)
	b.ip.sweep(10 * time.Minute)
}

func hasTagValue(tags nostr.Tags, name, value string) bool {
	for _, tag := range tags {
		if len(tag) > 0 && tag[0] == name && (value == "" || len(tag) > 1 && tag[1] == value) {
			return true
		}
	}
	return false
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
