package main

import (
	"context"

	"github.com/nbd-wtf/go-nostr"
)

// privateGate gates kind-9002 (edit-metadata) and kind-9007 (create-group) events that carry
// the ['closed'] or ['private'] tags. These flags make a club invite-only and/or hidden from
// non-members respectively. Both require the club owner to have an active Premium subscription.
//
// Lapse behavior (mirrors entry-fee gate): once a club is closed/private the in-memory Group
// state keeps those flags until the relay restarts; only *new* flag-setting events are blocked.
// Auto-reopen on lapse is not implemented in v1.
type privateGate struct {
	prem       *premiumStore
	superadmin string
}

func newPrivateGate(prem *premiumStore, superadmin string) *privateGate {
	return &privateGate{prem: prem, superadmin: superadmin}
}

func (g *privateGate) reject(ctx context.Context, evt *nostr.Event) (bool, string) {
	if evt.Kind != kindCreateGroup && evt.Kind != nostr.KindSimpleGroupEditMetadata {
		return false, ""
	}
	// Only gate events that actually carry a closed/private flag.
	if evt.Tags.GetFirst([]string{"closed"}) == nil &&
		evt.Tags.GetFirst([]string{"private"}) == nil {
		return false, ""
	}
	// Superadmin is always exempt.
	if g.superadmin != "" && evt.PubKey == g.superadmin {
		return false, ""
	}
	// Premium check.
	if g.prem != nil && g.prem.valid(ctx, evt.PubKey) {
		return false, ""
	}
	return true, "restricted: private / invite-only clubs require zapclub Premium"
}
