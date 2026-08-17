package main

import (
	"context"
	"sync"

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
	mu         sync.Mutex
	listIdx    map[string]map[string]bool // pubkey → set of d-tags (distinct playlists)
}

func newPlaylistGate(db *badger.BadgerBackend, prem *premiumStore, superadmin string) *playlistGate {
	return &playlistGate{db: db, prem: prem, superadmin: superadmin, listIdx: map[string]map[string]bool{}}
}

// warmList seeds listIdx from BadgerDB on startup (one-time scan).
func (g *playlistGate) warmList(ctx context.Context) {
	ch, err := g.db.QueryEvents(ctx, nostr.Filter{Kinds: []int{30104}})
	if err != nil {
		return
	}
	g.mu.Lock()
	for ev := range ch {
		d := tagVal(ev, "d")
		if d == "" {
			continue
		}
		if g.listIdx[ev.PubKey] == nil {
			g.listIdx[ev.PubKey] = map[string]bool{}
		}
		g.listIdx[ev.PubKey][d] = true
	}
	g.mu.Unlock()
}

// observeEvent keeps listIdx current via OnEventSaved.
func (g *playlistGate) observeEvent(_ context.Context, ev *nostr.Event) {
	if ev.Kind != 30104 {
		return
	}
	d := tagVal(ev, "d")
	if d == "" {
		return
	}
	g.mu.Lock()
	if g.listIdx[ev.PubKey] == nil {
		g.listIdx[ev.PubKey] = map[string]bool{}
	}
	g.listIdx[ev.PubKey][d] = true
	g.mu.Unlock()
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
	incomingD := tagVal(evt, "d")
	g.mu.Lock()
	dTags := g.listIdx[evt.PubKey]
	_, exists := dTags[incomingD]
	count := len(dTags)
	g.mu.Unlock()
	if exists {
		return false, "" // updating an existing playlist — always allowed
	}
	if count >= maxPlaylistsFree {
		return true, "restricted: free accounts may save 1 playlist — upgrade to Premium for unlimited"
	}
	return false, ""
}
