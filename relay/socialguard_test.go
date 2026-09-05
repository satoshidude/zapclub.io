package main

import (
	"context"
	"testing"

	"github.com/fiatjaf/khatru"
	"github.com/fiatjaf/relay29"
	"github.com/nbd-wtf/go-nostr"
	"github.com/puzpuzpuz/xsync/v3"
)

const (
	testOwner  = "661419f8f48b1b496e2249aee97a6ad9d5bea907149dc7bf3eb7479f2bce555e"
	testMember = "b095f4347bab926917ccd36f371d1741e71d99079bb30562c2227dda29e0b8b1"
)

func socialGuardFixture() *socialGuard {
	state := &relay29.State{Groups: xsync.NewMapOf[string, *relay29.Group]()}
	state.Groups.Store("club", state.NewGroup("club", testOwner))
	return newSocialGuard(state, "")
}

func TestSocialGuardTracksMembership(t *testing.T) {
	g := socialGuardFixture()
	var published []int
	g.setMemberCountPublisher(func(_ string, count int) { published = append(published, count) })
	if !g.isMember("club", testOwner) {
		t.Fatal("owner must be seeded as a member")
	}

	g.observe(context.Background(), &nostr.Event{
		Kind: nostr.KindSimpleGroupPutUser,
		Tags: nostr.Tags{{"h", "club"}, {"p", testMember}},
	})
	if !g.isMember("club", testMember) {
		t.Fatal("put-user must add the member")
	}

	g.observe(context.Background(), &nostr.Event{
		Kind: nostr.KindSimpleGroupRemoveUser,
		Tags: nostr.Tags{{"h", "club"}, {"p", testMember}},
	})
	if g.isMember("club", testMember) {
		t.Fatal("remove-user must revoke membership immediately")
	}
	if got := g.memberCounts()["club"]; got != 1 {
		t.Fatalf("member count = %d, want 1", got)
	}
	if len(published) != 2 || published[0] != 2 || published[1] != 1 {
		t.Fatalf("published counts = %v, want [2 1]", published)
	}
}

func TestProtectedSocialKinds(t *testing.T) {
	for _, kind := range []int{kindChat, kindPresence, kindMembers} {
		if !isProtectedSocialKind(kind) {
			t.Fatalf("kind %d must be protected", kind)
		}
	}
	for _, kind := range []int{1, 39000, 30100, kindListenerBeat, kindListenerCount, kindMemberCount} {
		if isProtectedSocialKind(kind) {
			t.Fatalf("public kind %d must stay public", kind)
		}
	}
}

func TestEventGroupID(t *testing.T) {
	chat := &nostr.Event{Kind: kindChat, Tags: nostr.Tags{{"h", "chat-club"}}}
	members := &nostr.Event{Kind: kindMembers, Tags: nostr.Tags{{"d", "member-club"}}}
	if got := eventGroupID(chat); got != "chat-club" {
		t.Fatalf("chat group = %q", got)
	}
	if got := eventGroupID(members); got != "member-club" {
		t.Fatalf("members group = %q", got)
	}
}

func TestLiveChatAccessEndsWithMembership(t *testing.T) {
	g := socialGuardFixture()
	g.observe(context.Background(), &nostr.Event{
		Kind: nostr.KindSimpleGroupPutUser,
		Tags: nostr.Tags{{"h", "club"}, {"p", testMember}},
	})
	ws := &khatru.WebSocket{AuthedPublicKey: testMember}
	chat := &nostr.Event{Kind: kindChat, Tags: nostr.Tags{{"h", "club"}}}
	if g.preventBroadcast(ws, chat) {
		t.Fatal("current member must receive live chat")
	}
	g.observe(context.Background(), &nostr.Event{
		Kind: nostr.KindSimpleGroupRemoveUser,
		Tags: nostr.Tags{{"h", "club"}, {"p", testMember}},
	})
	if !g.preventBroadcast(ws, chat) {
		t.Fatal("removed member must stop receiving on an already-open connection")
	}
}

func TestReferenceQueryCannotLeakProtectedEvents(t *testing.T) {
	g := socialGuardFixture()
	next := func(context.Context, nostr.Filter) (chan *nostr.Event, error) {
		ch := make(chan *nostr.Event, 2)
		ch <- &nostr.Event{Kind: kindChat, Tags: nostr.Tags{{"h", "club"}}}
		ch <- &nostr.Event{Kind: 30100, Tags: nostr.Tags{{"h", "club"}}}
		close(ch)
		return ch, nil
	}
	out, err := g.protectQuery(next)(context.Background(), nostr.Filter{IDs: []string{"anything"}})
	if err != nil {
		t.Fatal(err)
	}
	var got []int
	for event := range out {
		got = append(got, event.Kind)
	}
	if len(got) != 1 || got[0] != 30100 {
		t.Fatalf("unauthenticated reference query returned kinds %v", got)
	}
}
