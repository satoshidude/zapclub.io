package main

import (
	"context"

	"github.com/fiatjaf/eventstore/badger"
	"github.com/nbd-wtf/go-nostr"
)

// clearRemovalBarOnJoin works around a relay29 design flaw that makes leaving a club
// permanent: both a voluntary leave (kind 9022) and a moderator kick (kind 9001) store a
// remove-user record, and relay29's ReactToJoinRequest then DENIES any later join request
// from that pubkey. Worse, its bar-check filters by pubkey ONLY — there is no group/`h`
// filter — so being removed from a SINGLE club silently blocks (re)joining EVERY club.
//
// In zapclub removal is never a permanent bar: open clubs let a user who left (or was
// kicked) come back, and PERMANENT exclusion is the relay-wide ban list. That ban check is
// an earlier RejectEvent, so it drops every event from a banned pubkey before it is ever
// saved — meaning this hook never runs for banned users and they stay locked out.
//
// So: on a join request (9021) from a non-banned pubkey, clear that user's stale
// remove-user records first. relay29's reactor runs AFTER this hook (same synchronous
// OnEventSaved pass), finds no removal on record, and admits the (re)join normally.
//
// MUST be registered in OnEventSaved BEFORE state.ReactToJoinRequest.
func clearRemovalBarOnJoin(db *badger.BadgerBackend) func(context.Context, *nostr.Event) {
	return func(ctx context.Context, evt *nostr.Event) {
		if evt.Kind != nostr.KindSimpleGroupJoinRequest {
			return
		}
		ch, err := db.QueryEvents(ctx, nostr.Filter{
			Kinds: []int{nostr.KindSimpleGroupRemoveUser},
			Tags:  nostr.TagMap{"p": []string{evt.PubKey}},
		})
		if err != nil {
			return
		}
		for old := range ch {
			_ = db.DeleteEvent(ctx, old)
		}
	}
}
