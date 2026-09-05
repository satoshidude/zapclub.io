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
	managerPubkey      string
	publishMemberCount func(groupID string, count int)
}

func (g *socialGuard) setMemberCountPublisher(publish func(groupID string, count int)) {
	g.mu.Lock()
	g.publishMemberCount = publish
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
	g := &socialGuard{members: make(map[string]map[string]struct{}), managerPubkey: managerPubkey}
	// main wires the guard before the HTTP/WebSocket server starts, so relay29's
	// in-memory groups cannot be mutated while this startup snapshot is copied.
	for id, group := range state.Groups.Range {
		set := make(map[string]struct{}, len(group.Members))
		for pubkey := range group.Members {
			set[pubkey] = struct{}{}
		}
		g.members[id] = set
	}
	return g
}

func (g *socialGuard) canRead(kind int, groupID, pubkey string) bool {
	// The relay manager may inspect rosters through the separately protected
	// admin dashboard, but never receives chat or presence without membership.
	return (kind == kindMembers && pubkey != "" && pubkey == g.managerPubkey) || g.isMember(groupID, pubkey)
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
	return kind == kindChat || kind == kindPresence || kind == kindMembers
}

// observe runs before relay29's observers. In particular, a removed member is
// dropped before relay29 broadcasts its freshly generated 39002 member event.
func (g *socialGuard) observe(_ context.Context, event *nostr.Event) {
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
	switch event.Kind {
	case nostr.KindSimpleGroupCreateGroup:
		g.members[groupID] = map[string]struct{}{event.PubKey: {}}
		changed = true
	case nostr.KindSimpleGroupPutUser:
		set := g.members[groupID]
		if set == nil {
			set = make(map[string]struct{})
			g.members[groupID] = set
		}
		for _, tag := range event.Tags {
			if len(tag) > 1 && tag[0] == "p" && nostr.IsValidPublicKey(tag[1]) {
				if _, exists := set[tag[1]]; !exists {
					changed = true
				}
				set[tag[1]] = struct{}{}
			}
		}
	case nostr.KindSimpleGroupRemoveUser:
		set := g.members[groupID]
		for _, tag := range event.Tags {
			if len(tag) > 1 && tag[0] == "p" {
				if _, exists := set[tag[1]]; exists {
					changed = true
				}
				delete(set, tag[1])
			}
		}
	case nostr.KindSimpleGroupDeleteGroup:
		_, changed = g.members[groupID]
		delete(g.members, groupID)
	}
	count := len(g.members[groupID])
	publish := g.publishMemberCount
	g.mu.Unlock()
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
	authed := khatru.GetAuthed(ctx)
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
	authed := khatru.GetAuthed(ctx)
	if authed == "" {
		return true, "auth-required: club social data is for signed-in members only"
	}

	groupIDs := filter.Tags["h"]
	if len(groupIDs) == 0 {
		groupIDs = filter.Tags["d"]
	}
	for _, groupID := range groupIDs {
		kind := kindChat
		if len(filter.Kinds) == 1 {
			kind = filter.Kinds[0]
		}
		if !g.canRead(kind, groupID, authed) {
			return true, "restricted: club social data is for members only"
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
		authed := khatru.GetAuthed(ctx)
		go func() {
			defer close(output)
			for event := range input {
				if isProtectedSocialKind(event.Kind) && !g.canRead(event.Kind, eventGroupID(event), authed) {
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
	return isProtectedSocialKind(event.Kind) && !g.canRead(event.Kind, eventGroupID(event), ws.AuthedPublicKey)
}
