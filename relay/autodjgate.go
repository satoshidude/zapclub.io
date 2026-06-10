package main

import (
	"context"

	"github.com/fiatjaf/eventstore/badger"
	"github.com/nbd-wtf/go-nostr"
)

type autoDJGate struct {
	db         *badger.BadgerBackend
	prem       *premiumStore
	superadmin string
}

func newAutoDJGate(db *badger.BadgerBackend, prem *premiumStore, superadmin string) *autoDJGate {
	return &autoDJGate{db: db, prem: prem, superadmin: superadmin}
}

// reject blocks a kind-30105 Auto DJ arm/disarm event unless the author is the club's owner
// AND has an active premium subscription. Superadmin is exempt.
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
	owner := clubOwnerFromDB(ctx, g.db, club)
	if owner == "" || evt.PubKey != owner {
		return true, "restricted: auto-dj may only be set by the club owner"
	}
	if g.prem == nil || !g.prem.valid(ctx, evt.PubKey) {
		return true, "restricted: auto-dj is a premium feature — upgrade at zapclub.io"
	}
	return false, ""
}
