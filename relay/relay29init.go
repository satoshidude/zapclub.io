package main

import (
	"context"

	"github.com/fiatjaf/khatru"
	"github.com/fiatjaf/relay29"
	"github.com/nbd-wtf/go-nostr"
)

// relay29Adapter preserves relay29's archived, void BroadcastEvent interface
// while letting Zapclub use Khatru's final API, which returns the delivery count.
type relay29Adapter struct{ *khatru.Relay }

func (a relay29Adapter) BroadcastEvent(event *nostr.Event) {
	a.Relay.BroadcastEvent(event)
}

// Listener beats use an anonymous, tab-scoped signing key by design. Session-marked presence
// and stage events use a separate, fail-closed connection-bound authorization check. Everything
// else keeps relay29's normal author-is-member group rule.
func restrictGroupWrites(state *relay29.State, sessions *sessionEventPolicy) func(context.Context, *nostr.Event) (bool, string) {
	return func(ctx context.Context, event *nostr.Event) (bool, string) {
		if event.Kind == kindListenerBeat {
			return false, ""
		}
		if isSessionEventCandidate(event) {
			return sessions.reject(ctx, event)
		}
		return state.RestrictWritesBasedOnGroupRules(ctx, event)
	}
}

// initRelay29 is the small integration formerly provided by relay29/khatru29.
// It lives here because that archived adapter no longer compiles with the final
// Khatru fix needed for per-connection PreventBroadcast behavior.
func initRelay29(opts relay29.Options) (*khatru.Relay, *relay29.State, *sessionEventPolicy) {
	pubkey, _ := nostr.GetPublicKey(opts.SecretKey)
	state := relay29.New(opts)
	sessions := newSessionEventPolicy()
	relay := khatru.NewRelay()
	relay.Info.PubKey = pubkey
	relay.Info.SupportedNIPs = append(relay.Info.SupportedNIPs, 29)

	state.Relay = relay29Adapter{relay}
	state.GetAuthed = khatru.GetAuthed

	relay.StoreEvent = append(relay.StoreEvent, state.DB.SaveEvent)
	relay.QueryEvents = append(relay.QueryEvents,
		state.NormalEventQuery,
		state.MetadataQueryHandler,
		state.AdminsQueryHandler,
		state.MembersQueryHandler,
		state.RolesQueryHandler,
	)
	relay.DeleteEvent = append(relay.DeleteEvent, state.DB.DeleteEvent)
	relay.RejectFilter = append(relay.RejectFilter, state.RequireKindAndSingleGroupIDOrSpecificEventReference)
	relay.RejectEvent = append(relay.RejectEvent,
		state.RequireHTagForExistingGroup,
		state.RequireModerationEventsToBeRecent,
		restrictGroupWrites(state, sessions),
		state.RestrictInvalidModerationActions,
		state.PreventWritingOfEventsJustDeleted,
		state.CheckPreviousTag,
	)
	relay.OnEventSaved = append(relay.OnEventSaved,
		state.ApplyModerationAction,
		state.ReactToJoinRequest,
		state.ReactToLeaveRequest,
		state.AddToPreviousChecking,
	)
	relay.OnConnect = append(relay.OnConnect, khatru.RequestAuth)
	return relay, state, sessions
}
