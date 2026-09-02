package main

import (
	"context"
	"sync"

	"github.com/fiatjaf/eventstore/badger"
	"github.com/nbd-wtf/go-nostr"
)

const maxClubs = 3

// clubCap gates kind-9007 (create-group) events: accounts may own at most 3 clubs.
// Existing clubs beyond the limit are grandfathered; superadmin is exempt.
type clubCap struct {
	db         *badger.BadgerBackend
	superadmin string
	mu         sync.Mutex
	countIdx   map[string]int // pubkey → number of created clubs (9007)
}

func newClubCap(db *badger.BadgerBackend, superadmin string) *clubCap {
	return &clubCap{db: db, superadmin: superadmin, countIdx: map[string]int{}}
}

// warmCount seeds countIdx from BadgerDB on startup (one-time scan).
func (c *clubCap) warmCount(ctx context.Context) {
	ch, err := c.db.QueryEvents(ctx, nostr.Filter{Kinds: []int{kindCreateGroup}})
	if err != nil {
		return
	}
	c.mu.Lock()
	for ev := range ch {
		c.countIdx[ev.PubKey]++
	}
	c.mu.Unlock()
}

// observeEvent keeps countIdx current via OnEventSaved.
func (c *clubCap) observeEvent(_ context.Context, ev *nostr.Event) {
	if ev.Kind != kindCreateGroup {
		return
	}
	c.mu.Lock()
	c.countIdx[ev.PubKey]++
	c.mu.Unlock()
}

func (c *clubCap) reject(_ context.Context, evt *nostr.Event) (bool, string) {
	if evt.Kind != kindCreateGroup {
		return false, ""
	}
	if c.superadmin != "" && evt.PubKey == c.superadmin {
		return false, ""
	}
	c.mu.Lock()
	count := c.countIdx[evt.PubKey]
	c.mu.Unlock()
	if count >= maxClubs {
		return true, "too many clubs: accounts may own up to 3 clubs"
	}
	return false, ""
}
