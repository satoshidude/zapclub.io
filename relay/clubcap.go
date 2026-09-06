package main

import (
	"context"
	"sync"

	"github.com/nbd-wtf/go-nostr"
)

const maxClubs = 3

// clubCap gates kind-9007 (create-group) events: accounts may own at most 3 clubs.
// Existing clubs beyond the limit are grandfathered; superadmin is exempt.
type clubCap struct {
	db         clubCapStore
	superadmin string
	mu         sync.Mutex
	countIdx   map[string]int    // pubkey → number of created clubs (9007)
	createIDs  map[string]string // accepted event id → owner; reload/observe overlap is idempotent
}

type clubCapStore interface {
	QueryEvents(context.Context, nostr.Filter) (chan *nostr.Event, error)
}

func newClubCap(db clubCapStore, superadmin string) *clubCap {
	return &clubCap{
		db: db, superadmin: superadmin,
		countIdx:  map[string]int{},
		createIDs: map[string]string{},
	}
}

// warmCount seeds countIdx from BadgerDB on startup (one-time scan).
func (c *clubCap) warmCount(ctx context.Context) {
	_ = c.reload(ctx)
}

func (c *clubCap) reload(ctx context.Context) error {
	// Serialize the full snapshot-and-swap with OnEventSaved. Locking only the assignment can
	// lose an increment that lands after the query snapshot but before countIdx is replaced.
	c.mu.Lock()
	defer c.mu.Unlock()
	ch, err := c.db.QueryEvents(ctx, nostr.Filter{Kinds: []int{kindCreateGroup}})
	if err != nil {
		return err
	}
	next := map[string]int{}
	nextIDs := map[string]string{}
	for ev := range ch {
		if ev.ID != "" {
			if _, seen := nextIDs[ev.ID]; seen {
				continue
			}
			nextIDs[ev.ID] = ev.PubKey
		}
		next[ev.PubKey]++
	}
	c.countIdx = next
	c.createIDs = nextIDs
	return nil
}

// observeEvent keeps countIdx current via OnEventSaved.
func (c *clubCap) observeEvent(_ context.Context, ev *nostr.Event) {
	if ev.Kind != kindCreateGroup {
		return
	}
	c.mu.Lock()
	if ev.ID != "" {
		if _, seen := c.createIDs[ev.ID]; seen {
			c.mu.Unlock()
			return
		}
		c.createIDs[ev.ID] = ev.PubKey
	}
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
