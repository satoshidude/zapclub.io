package main

import (
	"context"
	"sync"

	"github.com/fiatjaf/khatru"
	"github.com/fiatjaf/relay29"
	"github.com/nbd-wtf/go-nostr"
)

const (
	kindChat        = 9
	kindMembers     = 39002
	kindMemberCount = 30112 // relay-signed public aggregate; never contains member identities
)

// socialGuard keeps the public radio stream public while protecting the social
// layer around it. Chat, presence and the member roster are visible only to an
// authenticated pubkey that is currently a member of that exact club.
//
// relay29 only applies this rule to groups marked "private". Zapclub also needs
// it for public clubs, so the guard owns a small, lock-protected membership index
// and applies the rule consistently to writes, history queries and live pushes.
type socialGuard struct {
	mu                 sync.RWMutex
	members            map[string]map[string]struct{} // group id -> member pubkeys
	moderators         map[string]map[string]struct{} // current owners/moderators allowed to inspect joins
	managerPubkey      string
	publishMemberCount func(groupID string, count int)
	revokeMember       func(context.Context, string, string) // ctx, group id, pubkey
	isBanned           func(pubkey string) bool
	getAuthed          func(context.Context) string
}

func (g *socialGuard) setMemberCountPublisher(publish func(groupID string, count int)) {
	g.mu.Lock()
	g.publishMemberCount = publish
	g.mu.Unlock()
}

func (g *socialGuard) setMemberRevoker(revoke func(context.Context, string, string)) {
	g.mu.Lock()
	g.revokeMember = revoke
	g.mu.Unlock()
}

func (g *socialGuard) setBanChecker(isBanned func(pubkey string) bool) {
	g.mu.Lock()
	g.isBanned = isBanned
	g.mu.Unlock()
}

// deleteClub mirrors an administrative deletion that bypasses relay29's normal kind-9008
// observer chain. It intentionally does not publish a zero member aggregate: the durable purge
// must be the final event transition for a deleted club.
func (g *socialGuard) deleteClub(groupID string) {
	if groupID == "" {
		return
	}
	g.mu.Lock()
	delete(g.members, groupID)
	delete(g.moderators, groupID)
	g.mu.Unlock()
}

func (g *socialGuard) memberCounts() map[string]int {
	g.mu.RLock()
	defer g.mu.RUnlock()
	counts := make(map[string]int, len(g.members))
	for groupID, members := range g.members {
		counts[groupID] = len(members)
	}
	return counts
}

func newSocialGuard(state *relay29.State, managerPubkey string) *socialGuard {
	g := &socialGuard{
		members:       make(map[string]map[string]struct{}),
		moderators:    make(map[string]map[string]struct{}),
		managerPubkey: managerPubkey,
		getAuthed:     khatru.GetAuthed,
	}
	// main wires the guard before the HTTP/WebSocket server starts, so relay29's
	// in-memory groups cannot be mutated while this startup snapshot is copied.
	for id, group := range state.Groups.Range {
		set := make(map[string]struct{}, len(group.Members))
		moderators := make(map[string]struct{})
		for pubkey, roles := range group.Members {
			set[pubkey] = struct{}{}
			for _, role := range roles {
				if role != nil && (role.Name == "owner" || role.Name == "moderator") {
					moderators[pubkey] = struct{}{}
					break
				}
			}
		}
		g.members[id] = set
		g.moderators[id] = moderators
	}
	return g
}

func (g *socialGuard) canRead(kind int, groupID, pubkey string) bool {
	if groupID == "" || pubkey == "" {
		return false
	}
	g.mu.RLock()
	isBanned := g.isBanned
	manager := g.managerPubkey
	_, member := g.members[groupID][pubkey]
	_, moderator := g.moderators[groupID][pubkey]
	g.mu.RUnlock()
	if isBanned != nil && isBanned(pubkey) {
		return false
	}
	switch kind {
	case kindJoinRequest:
		// Join requests can carry paid-entry proof material and are moderation inbox data.
		return moderator
	case kindMembers:
		// The relay manager may inspect generated rosters through the separately protected
		// admin dashboard, but never receives chat or membership history without membership.
		return pubkey == manager || member
	default:
		return member
	}
}

func (g *socialGuard) isMember(groupID, pubkey string) bool {
	if groupID == "" || pubkey == "" {
		return false
	}
	g.mu.RLock()
	_, ok := g.members[groupID][pubkey]
	g.mu.RUnlock()
	return ok
}

func eventGroupID(event *nostr.Event) string {
	if event == nil {
		return ""
	}
	name := "h"
	if event.Kind == kindMembers {
		name = "d"
	}
	if tag := event.Tags.GetFirst([]string{name, ""}); tag != nil {
		return tag.Value()
	}
	return ""
}

func isProtectedSocialKind(kind int) bool {
	switch kind {
	case kindChat,
		kindPresence,
		kindMembers,
		nostr.KindSimpleGroupPutUser,
		nostr.KindSimpleGroupRemoveUser,
		nostr.KindSimpleGroupJoinRequest,
		nostr.KindSimpleGroupLeaveRequest:
		return true
	default:
		return false
	}
}

// observe runs before relay29's observers. In particular, a removed member is
// dropped before relay29 broadcasts its freshly generated 39002 member event.
func (g *socialGuard) observe(ctx context.Context, event *nostr.Event) {
	if event == nil {
		return
	}
	groupID := ""
	if tag := event.Tags.GetFirst([]string{"h", ""}); tag != nil {
		groupID = tag.Value()
	}
	if groupID == "" {
		return
	}

	g.mu.Lock()
	changed := false
	revoked := make(map[string]struct{})
	switch event.Kind {
	case nostr.KindSimpleGroupCreateGroup:
		g.members[groupID] = map[string]struct{}{event.PubKey: {}}
		g.moderators[groupID] = map[string]struct{}{event.PubKey: {}}
		changed = true
	case nostr.KindSimpleGroupPutUser:
		set := g.members[groupID]
		if set == nil {
			set = make(map[string]struct{})
			g.members[groupID] = set
		}
		moderators := g.moderators[groupID]
		if moderators == nil {
			moderators = make(map[string]struct{})
			g.moderators[groupID] = moderators
		}
		for _, tag := range event.Tags {
			if len(tag) > 1 && tag[0] == "p" && nostr.IsValidPublicKey(tag[1]) {
				if _, exists := set[tag[1]]; !exists {
					changed = true
				}
				set[tag[1]] = struct{}{}
				isModerator := false
				for _, role := range tag[2:] {
					if role == "owner" || role == "moderator" {
						isModerator = true
						break
					}
				}
				if isModerator {
					moderators[tag[1]] = struct{}{}
				} else {
					delete(moderators, tag[1])
				}
			}
		}
	case nostr.KindSimpleGroupRemoveUser:
		set := g.members[groupID]
		for _, tag := range event.Tags {
			if len(tag) > 1 && tag[0] == "p" {
				if _, exists := set[tag[1]]; exists {
					changed = true
					revoked[tag[1]] = struct{}{}
				}
				delete(set, tag[1])
				delete(g.moderators[groupID], tag[1])
			}
		}
	case nostr.KindSimpleGroupDeleteGroup:
		for pubkey := range g.members[groupID] {
			revoked[pubkey] = struct{}{}
		}
		_, changed = g.members[groupID]
		delete(g.members, groupID)
		delete(g.moderators, groupID)
	}
	count := len(g.members[groupID])
	publish := g.publishMemberCount
	revoke := g.revokeMember
	g.mu.Unlock()
	if revoke != nil {
		for pubkey := range revoked {
			revoke(ctx, groupID, pubkey)
		}
	}
	if changed && publish != nil {
		publish(groupID, count)
	}
}

// rejectChatWrite requires both a valid NIP-42 connection identity and current
// club membership. The event signature alone is not treated as a login session.
func (g *socialGuard) rejectChatWrite(ctx context.Context, event *nostr.Event) (bool, string) {
	if event.Kind != kindChat && event.Kind != kindMood {
		return false, ""
	}
	feature := "club chat"
	if event.Kind == kindMood {
		feature = "vibemeter"
	}
	authed := g.authed(ctx)
	if authed == "" {
		return true, "auth-required: sign in before using the " + feature
	}
	if authed != event.PubKey {
		return true, "restricted: authenticated pubkey does not match event author"
	}
	if !g.isMember(eventGroupID(event), authed) {
		return true, "restricted: " + feature + " is for members only"
	}
	return false, ""
}

// rejectRead triggers NIP-42 before an explicit protected subscription. The
// query wrapper below is still the final boundary because an ids/#e filter may
// not disclose the event kind or group until after the database lookup.
func (g *socialGuard) rejectRead(ctx context.Context, filter nostr.Filter) (bool, string) {
	wantsProtected := false
	for _, kind := range filter.Kinds {
		if isProtectedSocialKind(kind) {
			wantsProtected = true
			break
		}
	}
	if !wantsProtected {
		return false, ""
	}
	authed := g.authed(ctx)
	if authed == "" {
		return true, "auth-required: club social data is for signed-in members only"
	}

	groupIDs := filter.Tags["h"]
	if len(groupIDs) == 0 {
		groupIDs = filter.Tags["d"]
	}
	for _, groupID := range groupIDs {
		for _, kind := range filter.Kinds {
			if isProtectedSocialKind(kind) &&
				!g.canRead(kind, groupID, authed) &&
				!g.isSelfMembershipFilter(kind, filter, authed) {
				return true, "restricted: club social data is not available to this account"
			}
		}
	}
	return false, ""
}

// protectQuery filters every relay query handler, including relay29's generated
// 39002 events and reference queries by event id. This makes the rule independent
// of the shape of a client filter.
func (g *socialGuard) protectQuery(
	next func(context.Context, nostr.Filter) (chan *nostr.Event, error),
) func(context.Context, nostr.Filter) (chan *nostr.Event, error) {
	return func(ctx context.Context, filter nostr.Filter) (chan *nostr.Event, error) {
		input, err := next(ctx, filter)
		if err != nil || input == nil {
			return input, err
		}
		output := make(chan *nostr.Event)
		authed := g.authed(ctx)
		go func() {
			defer close(output)
			for event := range input {
				if isProtectedSocialKind(event.Kind) &&
					!g.canRead(event.Kind, eventGroupID(event), authed) &&
					!g.canReadOwnTransition(event, authed) {
					continue
				}
				output <- event
			}
		}()
		return output, nil
	}
}

// preventBroadcast re-checks membership for every live event. Leave/kick takes
// effect immediately even when the removed browser keeps its old REQ open.
func (g *socialGuard) preventBroadcast(ws *khatru.WebSocket, event *nostr.Event) bool {
	pubkey := ws.AuthedPublicKey
	if !isProtectedSocialKind(event.Kind) {
		return false
	}
	if g.banned(pubkey) {
		return true
	}
	// A multi-target mutation that includes this account is never a safe self transition: it
	// would disclose the other targets at the exact moment a non-member is being admitted.
	if (event.Kind == nostr.KindSimpleGroupPutUser || event.Kind == nostr.KindSimpleGroupRemoveUser) &&
		eventTargets(event, pubkey) && !eventTargetsOnly(event, pubkey) {
		return true
	}
	if g.canRead(event.Kind, eventGroupID(event), pubkey) {
		return false
	}
	// social.observe removes the target before relay29 broadcasts a kick/leave transition.
	// The same exact-target exception lets an open-club joiner receive its generated 9000 and
	// then load the protected roster. It never grants unrelated membership history.
	return !g.canReadOwnTransition(event, pubkey)
}

func (g *socialGuard) banned(pubkey string) bool {
	g.mu.RLock()
	isBanned := g.isBanned
	g.mu.RUnlock()
	return isBanned != nil && isBanned(pubkey)
}

func (g *socialGuard) authed(ctx context.Context) string {
	if g.getAuthed != nil {
		return g.getAuthed(ctx)
	}
	return khatru.GetAuthed(ctx)
}

func (g *socialGuard) isSelfMembershipFilter(kind int, filter nostr.Filter, pubkey string) bool {
	if pubkey == "" || g.banned(pubkey) ||
		(kind != nostr.KindSimpleGroupPutUser && kind != nostr.KindSimpleGroupRemoveUser) {
		return false
	}
	targets := filter.Tags["p"]
	return len(targets) == 1 && targets[0] == pubkey
}

func (g *socialGuard) canReadOwnTransition(event *nostr.Event, pubkey string) bool {
	if event == nil || pubkey == "" || g.banned(pubkey) {
		return false
	}
	switch event.Kind {
	case nostr.KindSimpleGroupPutUser, nostr.KindSimpleGroupRemoveUser:
		return eventTargetsOnly(event, pubkey)
	case nostr.KindSimpleGroupLeaveRequest:
		return event.PubKey == pubkey && !eventHasForeignTarget(event, pubkey)
	default:
		return false
	}
}

func eventTargets(event *nostr.Event, pubkey string) bool {
	for _, tag := range event.Tags.GetAll([]string{"p", ""}) {
		if tag.Value() == pubkey {
			return true
		}
	}
	return false
}

func eventTargetsOnly(event *nostr.Event, pubkey string) bool {
	targets := event.Tags.GetAll([]string{"p", ""})
	if len(targets) == 0 {
		return false
	}
	for _, tag := range targets {
		if tag.Value() != pubkey {
			return false
		}
	}
	return true
}

func eventHasForeignTarget(event *nostr.Event, pubkey string) bool {
	for _, tag := range event.Tags.GetAll([]string{"p", ""}) {
		if tag.Value() != pubkey {
			return true
		}
	}
	return false
}
