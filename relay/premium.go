package main

import (
	"context"
	"log"
	"strconv"
	"time"

	"github.com/fiatjaf/eventstore/badger"
	"github.com/fiatjaf/khatru"
	"github.com/nbd-wtf/go-nostr"
)

const kindPremium = 30108 // relay-authored, d=<subscriber-pubkey>, premium_until=<unix-sec>

// premiumStore manages premium subscriptions as relay-signed kind-30108 events.
// Clients trust 30108 whose pubkey == CLUB_RELAY_PUBKEY, the same way they trust now_playing.
type premiumStore struct {
	db    *badger.BadgerBackend
	relay *khatru.Relay
	sk    string // relay secret key
	pub   string // relay pubkey
}

func newPremiumStore(db *badger.BadgerBackend, relay *khatru.Relay, sk, pub string) *premiumStore {
	return &premiumStore{db: db, relay: relay, sk: sk, pub: pub}
}

// valid reports whether the user currently has an active premium subscription.
func (p *premiumStore) valid(ctx context.Context, pubkey string) bool {
	until := p.until(ctx, pubkey)
	return until > time.Now().Unix()
}

// until returns the unix-second timestamp of subscription expiry for pubkey, or 0 if none.
func (p *premiumStore) until(ctx context.Context, pubkey string) int64 {
	ch, err := p.db.QueryEvents(ctx, nostr.Filter{
		Kinds:   []int{kindPremium},
		Authors: []string{p.pub},
		Tags:    nostr.TagMap{"d": []string{pubkey}},
	})
	if err != nil {
		return 0
	}
	var newest *nostr.Event
	for ev := range ch {
		if newest == nil || ev.CreatedAt > newest.CreatedAt {
			newest = ev
		}
	}
	if newest == nil {
		return 0
	}
	t := newest.Tags.Find("premium_until")
	if t == nil {
		return 0
	}
	v, err := strconv.ParseInt(t[1], 10, 64)
	if err != nil {
		return 0
	}
	return v
}

// grant extends (or creates) a premium subscription for pubkey by months calendar months,
// stacking on top of any existing expiry. Writes a relay-signed kind-30108 event.
func (p *premiumStore) grant(ctx context.Context, pubkey string, months int) {
	base := time.Now().Unix()
	if existing := p.until(ctx, pubkey); existing > base {
		base = existing
	}
	until := addMonths(base, months)

	ev := &nostr.Event{
		Kind:      kindPremium,
		CreatedAt: nostr.Now(),
		Tags: nostr.Tags{
			{"d", pubkey},
			{"premium_until", strconv.FormatInt(until, 10)},
		},
		Content: "",
	}
	if err := ev.Sign(p.sk); err != nil {
		log.Printf("premium sign: %v", err)
		return
	}
	if err := p.db.ReplaceEvent(ctx, ev); err != nil {
		log.Printf("premium store: %v", err)
		return
	}
	p.relay.BroadcastEvent(ev)
}

// expiringSoon returns all (pubkey, premium_until) pairs that expire within the given window
// and still have active subscriptions. Used for renewal-reminder DMs.
func (p *premiumStore) expiringSoon(ctx context.Context, within time.Duration) []premiumEntry {
	now := time.Now().Unix()
	deadline := now + int64(within.Seconds())
	ch, err := p.db.QueryEvents(ctx, nostr.Filter{
		Kinds:   []int{kindPremium},
		Authors: []string{p.pub},
	})
	if err != nil {
		return nil
	}
	// newest-per-d (the store may have old replaced events during a scan)
	best := map[string]*nostr.Event{}
	for ev := range ch {
		t := ev.Tags.Find("d")
		if t == nil {
			continue
		}
		pk := t[1]
		if prev, ok := best[pk]; !ok || ev.CreatedAt > prev.CreatedAt {
			best[pk] = ev
		}
	}
	var out []premiumEntry
	for pk, ev := range best {
		t := ev.Tags.Find("premium_until")
		if t == nil {
			continue
		}
		until, err := strconv.ParseInt(t[1], 10, 64)
		if err != nil {
			continue
		}
		if until >= now && until <= deadline {
			out = append(out, premiumEntry{Pubkey: pk, Until: until})
		}
	}
	return out
}

type premiumEntry struct {
	Pubkey string
	Until  int64
}

// addMonths adds n calendar months to the unix timestamp t.
func addMonths(t int64, n int) int64 {
	return time.Unix(t, 0).UTC().AddDate(0, n, 0).Unix()
}
