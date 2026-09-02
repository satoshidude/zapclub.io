package main

import (
	"context"

	"github.com/fiatjaf/eventstore/badger"
	"github.com/nbd-wtf/go-nostr"
)

type autoDJGate struct {
	db         *badger.BadgerBackend
	superadmin string
	// ownerFn is wired from the conductor after init so reject() uses the cached
	// owner lookup (in-memory → SQLite → BadgerDB) instead of a raw DB scan.
	ownerFn func(ctx context.Context, club string) string
}

func newAutoDJGate(db *badger.BadgerBackend, superadmin string) *autoDJGate {
	return &autoDJGate{db: db, superadmin: superadmin}
}

// reject blocks a kind-30105 Auto DJ arm/disarm event unless the author is the club's owner
// Superadmin is exempt.
func (g *autoDJGate) reject(ctx context.Context, evt *nostr.Event) (bool, string) {
	if evt.Kind != kindAutoDJ {
		return false, ""
	}
	if g.superadmin != "" && evt.PubKey == g.superadmin {
		return false, ""
	}
	club := tagVal(evt, "h")
	if club == "" {
		return true, "restricted: auto-dj event missing h-tag"
	}
	var owner string
	if g.ownerFn != nil {
		owner = g.ownerFn(ctx, club)
	} else {
		owner = clubOwnerFromDB(ctx, g.db, club)
	}
	if owner == "" || evt.PubKey != owner {
		return true, "restricted: auto-dj may only be set by the club owner"
	}
	return false, ""
}
