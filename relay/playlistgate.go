package main

import (
	"context"

	"github.com/fiatjaf/eventstore/badger"
	"github.com/nbd-wtf/go-nostr"
)

const maxPlaylistsFree = 1

// playlistGate limits kind-30104 (saved playlist library) events. Free accounts may save
// 1 playlist; premium accounts are unlimited. Updates to an existing playlist (same d-tag)
// are always allowed. Superadmin is exempt.
type playlistGate struct {
	db         *badger.BadgerBackend
	prem       *premiumStore
	superadmin string
}

func newPlaylistGate(db *badger.BadgerBackend, prem *premiumStore, superadmin string) *playlistGate {
	return &playlistGate{db: db, prem: prem, superadmin: superadmin}
}

func (g *playlistGate) reject(ctx context.Context, evt *nostr.Event) (bool, string) {
	if evt.Kind != 30104 {
		return false, ""
	}
	if g.superadmin != "" && evt.PubKey == g.superadmin {
		return false, ""
	}
	if g.prem != nil && g.prem.valid(ctx, evt.PubKey) {
		return false, ""
	}
	// Count distinct saved playlists (d-tags) for this user.
	ch, err := g.db.QueryEvents(ctx, nostr.Filter{
		Kinds:   []int{30104},
		Authors: []string{evt.PubKey},
	})
	if err != nil {
		return false, ""
	}
	incomingD := tagVal(evt, "d")
	count := 0
	for ev := range ch {
		d := tagVal(ev, "d")
		if d == incomingD {
			return false, "" // updating an existing playlist — always allowed
		}
		count++
	}
	if count >= maxPlaylistsFree {
		return true, "restricted: free accounts may save 1 playlist — upgrade to Premium for unlimited"
	}
	return false, ""
}
