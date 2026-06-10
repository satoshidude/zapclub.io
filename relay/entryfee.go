package main

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"sync"
	"time"

	"github.com/fiatjaf/eventstore/badger"
	"github.com/nbd-wtf/go-nostr"
)

// Paid-club entry gate (the "Sats-Eintritts-Gate", relay-enforced). A club is "paid" when its
// OWNER-authored config event (kind 30101) sets access=paid with a price (sats) + the entry
// LNURL's NIP-57 zapper pubkey. Joining such a club (kind 9021) then requires a valid zap
// RECEIPT (9735) proving the joiner paid >= price to that wallet — verified here so a
// hand-crafted 9021 can't bypass it. The owner joins their own club free.
//
// The client attaches the full 9735 as a ['proof', <json>] tag on the 9021. We verify:
//   - 9735 has a valid id+signature and was signed by the club's configured zapper pubkey
//   - its embedded 9734 zap request is valid, signed by the JOINER (binds payment to them),
//     carries ['club_entry', <club>] (so a track-zap receipt can't be reused as entry), and
//     amount >= price
//   - the receipt is fresh (< 10min) and its id hasn't been used before (single-use; in-memory).

const (
	kindClubConfig  = 30101
	kindJoinRequest = 9021
	kindZapReceipt  = 9735
	kindZapRequest  = 9734
	kindCreateGroup = 9007
)

type entryGate struct {
	db   *badger.BadgerBackend
	used sync.Map // 9735 id → struct{} : single-use receipts (resets on restart, acceptable)
}

func newEntryGate(db *badger.BadgerBackend) *entryGate { return &entryGate{db: db} }

type clubAccess struct {
	owner  string
	paid   bool
	price  int // sats
	zapper string
}

func tagVal(ev *nostr.Event, key string) string {
	if t := ev.Tags.Find(key); t != nil {
		return t[1]
	}
	return ""
}

// newest returns the most recent stored event matching the filter, or nil.
func (g *entryGate) newest(ctx context.Context, f nostr.Filter) *nostr.Event {
	ch, err := g.db.QueryEvents(ctx, f)
	if err != nil {
		return nil
	}
	var newest *nostr.Event
	for ev := range ch {
		if newest == nil || ev.CreatedAt > newest.CreatedAt {
			newest = ev
		}
	}
	return newest
}

// access reads a club's owner + its owner-authored config (30101) from the local store. The
// owner is the creator (author of the kind-9007 create-group event — that's the only owner
// source actually persisted in badger; relay29 serves 39001 dynamically, not from the store).
// Joins are infrequent, so reading per-join (no cache) is fine.
// prem is set by main.go after both entryGate and premiumStore are initialized.
var entryPrem *premiumStore

func (g *entryGate) access(ctx context.Context, club string) clubAccess {
	create := g.newest(ctx, nostr.Filter{Kinds: []int{kindCreateGroup}, Tags: nostr.TagMap{"h": []string{club}}})
	ca := clubAccess{}
	if create == nil {
		return ca // unknown club → not gated
	}
	ca.owner = create.PubKey
	cfg := g.newest(ctx, nostr.Filter{
		Kinds:   []int{kindClubConfig},
		Authors: []string{ca.owner},
		Tags:    nostr.TagMap{"d": []string{club}},
	})
	if cfg == nil || tagVal(cfg, "access") != "paid" {
		return ca // no owner config or open → not gated
	}
	// Entry-Fee is a premium feature: ignore paid config if owner does not have premium.
	if entryPrem != nil && !entryPrem.valid(ctx, ca.owner) {
		return ca
	}
	ca.paid = true
	ca.price, _ = strconv.Atoi(tagVal(cfg, "price"))
	ca.zapper = tagVal(cfg, "zapper")
	return ca
}

// verifyReceipt checks an attached 9735 proves `joiner` paid >= price to the club's zapper.
func (g *entryGate) verifyReceipt(proofJSON, club string, ca clubAccess, joiner string) (bool, string) {
	var r nostr.Event
	if err := json.Unmarshal([]byte(proofJSON), &r); err != nil {
		return false, "invalid entry proof"
	}
	if r.Kind != kindZapReceipt {
		return false, "entry proof is not a zap receipt"
	}
	if !r.CheckID() {
		return false, "entry proof id mismatch"
	}
	if ok, _ := r.CheckSignature(); !ok {
		return false, "entry proof signature invalid"
	}
	if ca.zapper == "" || r.PubKey != ca.zapper {
		return false, "entry proof not from the club's entry wallet"
	}
	// Short freshness window: LNURL invoices expire in minutes anyway, and the replay-set is
	// in-memory (reset on restart) — a tight window keeps a post-restart replay of an old receipt
	// to a few minutes rather than an hour.
	if time.Since(r.CreatedAt.Time()) > 10*time.Minute {
		return false, "entry proof expired — pay again"
	}
	desc := tagVal(&r, "description")
	if desc == "" {
		return false, "entry proof missing the payment request"
	}
	var zr nostr.Event
	if err := json.Unmarshal([]byte(desc), &zr); err != nil || zr.Kind != kindZapRequest {
		return false, "entry proof has a bad payment request"
	}
	if !zr.CheckID() {
		return false, "payment request id mismatch"
	}
	if ok, _ := zr.CheckSignature(); !ok {
		return false, "payment request signature invalid"
	}
	if zr.PubKey != joiner {
		return false, "entry payment wasn't made by you"
	}
	if zr.Tags.FindWithValue("club_entry", club) == nil {
		return false, "entry payment is not for this club"
	}
	msat, _ := strconv.Atoi(tagVal(&zr, "amount"))
	if msat < ca.price*1000 {
		return false, fmt.Sprintf("entry payment too low (need %d sats)", ca.price)
	}
	return true, ""
}

// reject is the khatru RejectEvent gate for paid-club joins.
func (g *entryGate) reject(ctx context.Context, evt *nostr.Event) (bool, string) {
	if evt.Kind != kindJoinRequest {
		return false, ""
	}
	club := tagVal(evt, "h")
	if club == "" {
		return false, ""
	}
	ca := g.access(ctx, club)
	if !ca.paid {
		return false, "" // open club → free join
	}
	if evt.PubKey == ca.owner {
		return false, "" // the owner never pays to enter their own club
	}
	proof := evt.Tags.Find("proof")
	if proof == nil || len(proof) < 2 || proof[1] == "" {
		return true, fmt.Sprintf("payment required: this club charges %d sats to enter", ca.price)
	}
	if ok, why := g.verifyReceipt(proof[1], club, ca, evt.PubKey); !ok {
		return true, why
	}
	// Single-use: a paid receipt admits exactly one join.
	var r nostr.Event
	_ = json.Unmarshal([]byte(proof[1]), &r)
	if _, loaded := g.used.LoadOrStore(r.ID, struct{}{}); loaded {
		return true, "entry proof already used"
	}
	return false, ""
}
