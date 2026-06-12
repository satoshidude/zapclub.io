package main

import (
	"context"
	"database/sql"
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
	sk    string   // relay secret key
	pub   string   // relay pubkey
	sq    *sql.DB  // SQLite cache; nil = disabled (graceful degradation)
}

func newPremiumStore(db *badger.BadgerBackend, relay *khatru.Relay, sk, pub string) *premiumStore {
	return &premiumStore{db: db, relay: relay, sk: sk, pub: pub}
}

// sqPremGet returns (valid, found) from the SQLite cache. found=false on miss or TTL expiry.
func (p *premiumStore) sqPremGet(pubkey string) (bool, bool) {
	if p.sq == nil {
		return false, false
	}
	var valid int
	var expires int64
	err := p.sq.QueryRow(`SELECT valid, expires FROM premium_cache WHERE pubkey=?`, pubkey).Scan(&valid, &expires)
	if err != nil {
		return false, false
	}
	if expires <= time.Now().Unix() {
		return false, false
	}
	return valid != 0, true
}

// sqPremSet writes a premium status into the SQLite cache with the given TTL in seconds.
func (p *premiumStore) sqPremSet(pubkey string, isValid bool, ttlSec int64) {
	if p.sq == nil {
		return
	}
	v := 0
	if isValid {
		v = 1
	}
	expires := time.Now().Unix() + ttlSec
	if _, err := p.sq.Exec(
		`INSERT OR REPLACE INTO premium_cache(pubkey,valid,expires) VALUES(?,?,?)`,
		pubkey, v, expires,
	); err != nil {
		log.Printf("premium_cache set [%.8s]: %v", pubkey, err)
	}
}

// sqPremInvalidate removes the SQLite cache entry for pubkey so the next valid() call
// re-queries BadgerDB (used immediately after grant()).
func (p *premiumStore) sqPremInvalidate(pubkey string) {
	if p.sq == nil {
		return
	}
	p.sq.Exec(`DELETE FROM premium_cache WHERE pubkey=?`, pubkey)
}

// valid reports whether the user currently has an active premium subscription.
// Check order: SQLite cache (1 h TTL) → BadgerDB scan → write SQLite cache.
func (p *premiumStore) valid(ctx context.Context, pubkey string) bool {
	if v, ok := p.sqPremGet(pubkey); ok {
		return v
	}
	until := p.until(ctx, pubkey)
	result := until > time.Now().Unix()
	p.sqPremSet(pubkey, result, 3600)
	return result
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
	p.sqPremInvalidate(pubkey)
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
