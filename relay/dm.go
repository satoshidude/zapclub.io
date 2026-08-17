package main

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/nbd-wtf/go-nostr"
	"github.com/nbd-wtf/go-nostr/keyer"
	"github.com/nbd-wtf/go-nostr/nip17"
)

// dmSender sends NIP-17 gift-wrapped DMs from the relay key to subscribers.
// Used for: payment confirmation, expiry reminders.
type dmSender struct {
	kr   nostr.Keyer
	pub  string
	pool *nostr.SimplePool
}

func newDMSender(sk string) (*dmSender, error) {
	pool := nostr.NewSimplePool(context.Background())
	kr, err := keyer.New(context.Background(), pool, sk, nil)
	if err != nil {
		return nil, fmt.Errorf("dm keyer: %w", err)
	}
	pub, err := nostr.GetPublicKey(sk)
	if err != nil {
		return nil, fmt.Errorf("dm pubkey: %w", err)
	}
	return &dmSender{kr: kr, pub: pub, pool: pool}, nil
}

// send sends a NIP-17 DM to the recipient. Falls back to the well-known relay list when
// GetDMRelays returns nothing. Best-effort: logs errors, never panics.
func (d *dmSender) send(ctx context.Context, recipientPubkey, content string) {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()

	theirRelays := nip17.GetDMRelays(ctx, recipientPubkey, d.pool, wellKnownRelays)
	if len(theirRelays) == 0 {
		theirRelays = wellKnownRelays
	}
	err := nip17.PublishMessage(
		ctx,
		content,
		nostr.Tags{},
		d.pool,
		wellKnownRelays,
		theirRelays,
		d.kr,
		recipientPubkey,
		nil,
	)
	if err != nil {
		log.Printf("dm send to %s…: %v", recipientPubkey[:8], err)
	}
}

// wellKnownRelays are the fallback DM relays if the recipient has none in their profile.
var wellKnownRelays = []string{
	"wss://relay.damus.io",
	"wss://nos.lol",
	"wss://relay.primal.net",
}

// ── Reminder sweep ────────────────────────────────────────────────────────────

// reminded tracks pubkeys that have already received a renewal reminder this cycle.
// In-memory — resets on restart (acceptable: rare and produces at most one extra DM).
var reminded = &remindedSet{seen: map[string]bool{}}

type remindedSet struct {
	seen map[string]bool
}

func (r *remindedSet) check(pk string) bool  { return r.seen[pk] }
func (r *remindedSet) mark(pk string)        { r.seen[pk] = true }

// sendRenewalReminders checks all premium subscriptions expiring within 3 days and sends
// a DM reminder to those not yet reminded this cycle. Called from the main sweep ticker.
func sendRenewalReminders(ctx context.Context, prem *premiumStore, dm *dmSender) {
	entries := prem.expiringSoon(ctx, 3*24*time.Hour)
	for _, e := range entries {
		if reminded.check(e.Pubkey) {
			continue
		}
		expiry := time.Unix(e.Until, 0).UTC().Format("2006-01-02")
		msg := fmt.Sprintf(
			"⚡ Your zapclub.io Premium expires on %s. "+
				"Open https://zapclub.io to renew (2,100 sats/month).",
			expiry,
		)
		dm.send(ctx, e.Pubkey, msg)
		reminded.mark(e.Pubkey)
	}
}
