package main

import (
	"context"

	"github.com/fiatjaf/eventstore/badger"
	"github.com/nbd-wtf/go-nostr"
)

const maxClubsPerUser = 3

// clubCap gates kind-9007 (create-group) events: a user may own at most maxClubsPerUser clubs.
// Superadmin (SUPERADMIN env) is exempt.
type clubCap struct {
	db         *badger.BadgerBackend
	superadmin string
}

func newClubCap(db *badger.BadgerBackend, superadmin string) *clubCap {
	return &clubCap{db: db, superadmin: superadmin}
}

func (c *clubCap) reject(ctx context.Context, evt *nostr.Event) (bool, string) {
	if evt.Kind != kindCreateGroup {
		return false, ""
	}
	if c.superadmin != "" && evt.PubKey == c.superadmin {
		return false, ""
	}
	ch, err := c.db.QueryEvents(ctx, nostr.Filter{
		Kinds:   []int{kindCreateGroup},
		Authors: []string{evt.PubKey},
	})
	if err != nil {
		return false, ""
	}
	count := 0
	for range ch {
		count++
	}
	if count >= maxClubsPerUser {
		return true, "too many clubs: free accounts may own up to 3 clubs"
	}
	return false, ""
}
