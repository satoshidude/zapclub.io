package main

import (
	"context"

	"github.com/fiatjaf/eventstore/badger"
	"github.com/nbd-wtf/go-nostr"
)

const (
	maxClubsFree    = 1
	maxClubsPremium = 3
)

// clubCap gates kind-9007 (create-group) events: free accounts may own at most 1 club,
// premium accounts at most 3. Existing clubs beyond the limit are grandfathered — only
// new creation is blocked. Superadmin (SUPERADMIN env) is exempt.
type clubCap struct {
	db         *badger.BadgerBackend
	prem       *premiumStore
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
	cap := maxClubsFree
	if c.prem != nil && c.prem.valid(ctx, evt.PubKey) {
		cap = maxClubsPremium
	}
	if count >= cap {
		if cap == maxClubsFree {
			return true, "too many clubs: free accounts may own 1 club — upgrade to Premium for up to 3"
		}
		return true, "too many clubs: premium accounts may own up to 3 clubs"
	}
	return false, ""
}
