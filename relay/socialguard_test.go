package main

import (
	"context"
	"testing"

	"github.com/fiatjaf/khatru"
	"github.com/fiatjaf/relay29"
	"github.com/nbd-wtf/go-nostr"
	"github.com/nbd-wtf/go-nostr/nip29"
	"github.com/puzpuzpuz/xsync/v3"
)

const (
	testOwner  = "661419f8f48b1b496e2249aee97a6ad9d5bea907149dc7bf3eb7479f2bce555e"
	testMember = "b095f4347bab926917ccd36f371d1741e71d99079bb30562c2227dda29e0b8b1"
)

func socialGuardFixture() *socialGuard {
	state := &relay29.State{Groups: xsync.NewMapOf[string, *relay29.Group]()}
	group := state.NewGroup("club", testOwner)
	group.Members[testOwner] = []*nip29.Role{{Name: "owner"}}
	state.Groups.Store("club", group)
	return newSocialGuard(state, "")
}

func TestSocialGuardTracksMembership(t *testing.T) {
	g := socialGuardFixture()
	var published []int
	var revoked []string
	g.setMemberCountPublisher(func(_ string, count int) { published = append(published, count) })
	g.setMemberRevoker(func(_ context.Context, club, pubkey string) {
		revoked = append(revoked, club+":"+pubkey)
	})
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
	if len(revoked) != 1 || revoked[0] != "club:"+testMember {
		t.Fatalf("stage revocations = %v, want current removed member", revoked)
	}
}

func TestProtectedSocialKinds(t *testing.T) {
	for _, kind := range []int{
		kindChat,
		kindPresence,
		kindMembers,
		nostr.KindSimpleGroupPutUser,
		nostr.KindSimpleGroupRemoveUser,
		nostr.KindSimpleGroupJoinRequest,
		nostr.KindSimpleGroupLeaveRequest,
	} {
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

func TestJoinRequestsAreVisibleOnlyToOwnerOrModerator(t *testing.T) {
	g := socialGuardFixture()
	g.observe(context.Background(), &nostr.Event{
		Kind: nostr.KindSimpleGroupPutUser,
		Tags: nostr.Tags{{"h", "club"}, {"p", testMember}},
	})
	if !g.canRead(kindJoinRequest, "club", testOwner) {
		t.Fatal("owner must be able to inspect join requests")
	}
	if g.canRead(kindJoinRequest, "club", testMember) {
		t.Fatal("plain member must not see join-request proof material")
	}
	g.observe(context.Background(), &nostr.Event{
		Kind: nostr.KindSimpleGroupPutUser,
		Tags: nostr.Tags{{"h", "club"}, {"p", testMember, "moderator"}},
	})
	if !g.canRead(kindJoinRequest, "club", testMember) {
		t.Fatal("moderator must be able to inspect join requests")
	}
	g.observe(context.Background(), &nostr.Event{
		Kind: nostr.KindSimpleGroupPutUser,
		Tags: nostr.Tags{{"h", "club"}, {"p", testMember}},
	})
	if g.canRead(kindJoinRequest, "club", testMember) {
		t.Fatal("role replacement must revoke join-request access")
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

func TestBannedMemberLosesProtectedButNotPublicReads(t *testing.T) {
	g := socialGuardFixture()
	g.observe(context.Background(), &nostr.Event{
		Kind: nostr.KindSimpleGroupPutUser,
		Tags: nostr.Tags{{"h", "club"}, {"p", testMember}},
	})
	banned := false
	g.setBanChecker(func(pubkey string) bool { return banned && pubkey == testMember })
	ws := &khatru.WebSocket{AuthedPublicKey: testMember}
	chat := &nostr.Event{Kind: kindChat, Tags: nostr.Tags{{"h", "club"}}}
	if !g.canRead(kindChat, "club", testMember) || g.preventBroadcast(ws, chat) {
		t.Fatal("current unbanned member must receive protected social data")
	}
	banned = true
	if g.canRead(kindChat, "club", testMember) || !g.preventBroadcast(ws, chat) {
		t.Fatal("ban must revoke protected history and an existing live subscription")
	}
	publicState := &nostr.Event{Kind: kindNowPlaying, Tags: nostr.Tags{{"h", "club"}}}
	if g.preventBroadcast(ws, publicState) {
		t.Fatal("ban must not turn public playback into protected data")
	}
	banned = false
	if !g.canRead(kindChat, "club", testMember) || g.preventBroadcast(ws, chat) {
		t.Fatal("unban must restore the unchanged membership's protected access")
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

func TestMembershipHistoryQueryFiltersByActualEventAndRole(t *testing.T) {
	g := socialGuardFixture()
	g.observe(context.Background(), &nostr.Event{
		Kind: nostr.KindSimpleGroupPutUser,
		Tags: nostr.Tags{{"h", "club"}, {"p", testMember}},
	})
	next := func(context.Context, nostr.Filter) (chan *nostr.Event, error) {
		ch := make(chan *nostr.Event, 5)
		for _, kind := range []int{
			nostr.KindSimpleGroupPutUser,
			nostr.KindSimpleGroupRemoveUser,
			nostr.KindSimpleGroupJoinRequest,
			nostr.KindSimpleGroupLeaveRequest,
			kindNowPlaying,
		} {
			ch <- &nostr.Event{Kind: kind, Tags: nostr.Tags{{"h", "club"}}}
		}
		close(ch)
		return ch, nil
	}

	tests := []struct {
		name   string
		authed string
		want   []int
	}{
		{name: "stranger", want: []int{kindNowPlaying}},
		{name: "member", authed: testMember, want: []int{
			nostr.KindSimpleGroupPutUser,
			nostr.KindSimpleGroupRemoveUser,
			nostr.KindSimpleGroupLeaveRequest,
			kindNowPlaying,
		}},
		{name: "owner", authed: testOwner, want: []int{
			nostr.KindSimpleGroupPutUser,
			nostr.KindSimpleGroupRemoveUser,
			nostr.KindSimpleGroupJoinRequest,
			nostr.KindSimpleGroupLeaveRequest,
			kindNowPlaying,
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g.getAuthed = func(context.Context) string { return tt.authed }
			out, err := g.protectQuery(next)(context.Background(), nostr.Filter{IDs: []string{"reference-query"}})
			if err != nil {
				t.Fatal(err)
			}
			var got []int
			for event := range out {
				got = append(got, event.Kind)
			}
			if len(got) != len(tt.want) {
				t.Fatalf("kinds = %v, want %v", got, tt.want)
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Fatalf("kinds = %v, want %v", got, tt.want)
				}
			}
		})
	}
}

func TestMembershipFiltersRequireAuthAndJoinModerationRole(t *testing.T) {
	g := socialGuardFixture()
	g.getAuthed = func(context.Context) string { return "" }
	for _, kind := range []int{
		nostr.KindSimpleGroupPutUser,
		nostr.KindSimpleGroupRemoveUser,
		nostr.KindSimpleGroupJoinRequest,
		nostr.KindSimpleGroupLeaveRequest,
	} {
		if rejected, _ := g.rejectRead(context.Background(), nostr.Filter{
			Kinds: []int{kind}, Tags: nostr.TagMap{"h": []string{"club"}},
		}); !rejected {
			t.Fatalf("unauthenticated kind %d query was not rejected", kind)
		}
	}

	g.observe(context.Background(), &nostr.Event{
		Kind: nostr.KindSimpleGroupPutUser,
		Tags: nostr.Tags{{"h", "club"}, {"p", testMember}},
	})
	g.getAuthed = func(context.Context) string { return testMember }
	if rejected, _ := g.rejectRead(context.Background(), nostr.Filter{
		Kinds: []int{kindJoinRequest}, Tags: nostr.TagMap{"h": []string{"club"}},
	}); !rejected {
		t.Fatal("plain member join-request query was not rejected")
	}
	g.getAuthed = func(context.Context) string { return testOwner }
	if rejected, reason := g.rejectRead(context.Background(), nostr.Filter{
		Kinds: []int{kindJoinRequest}, Tags: nostr.TagMap{"h": []string{"club"}},
	}); rejected {
		t.Fatalf("owner join-request query rejected: %s", reason)
	}
}

func TestRemovedMemberReceivesOnlyOwnLiveTransition(t *testing.T) {
	g := socialGuardFixture()
	g.observe(context.Background(), &nostr.Event{
		Kind: nostr.KindSimpleGroupPutUser,
		Tags: nostr.Tags{{"h", "club"}, {"p", testMember}},
	})
	kick := &nostr.Event{
		Kind: nostr.KindSimpleGroupRemoveUser,
		Tags: nostr.Tags{{"h", "club"}, {"p", testMember}},
	}
	g.observe(context.Background(), kick)
	removed := &khatru.WebSocket{AuthedPublicKey: testMember}
	if g.preventBroadcast(removed, kick) {
		t.Fatal("removed member must receive its own exact kick transition")
	}
	otherKick := &nostr.Event{
		Kind: nostr.KindSimpleGroupRemoveUser,
		Tags: nostr.Tags{{"h", "club"}, {"p", testOwner}},
	}
	if !g.preventBroadcast(removed, otherKick) {
		t.Fatal("removed member must not receive another member's transition")
	}
	leave := &nostr.Event{
		Kind:   nostr.KindSimpleGroupLeaveRequest,
		PubKey: testMember,
		Tags:   nostr.Tags{{"h", "club"}},
	}
	if g.preventBroadcast(removed, leave) {
		t.Fatal("leaver must receive its own exact leave transition")
	}
	g.setBanChecker(func(pubkey string) bool { return pubkey == testMember })
	if !g.preventBroadcast(removed, kick) {
		t.Fatal("global ban must override the self-transition exception")
	}
}

func TestNonMemberCanReadOnlyExactSelfPutTransition(t *testing.T) {
	g := socialGuardFixture()
	g.getAuthed = func(context.Context) string { return testMember }
	selfFilter := nostr.Filter{
		Kinds: []int{nostr.KindSimpleGroupPutUser},
		Tags: nostr.TagMap{
			"h": []string{"club"},
			"p": []string{testMember},
		},
	}
	if rejected, reason := g.rejectRead(context.Background(), selfFilter); rejected {
		t.Fatalf("exact authenticated self-put filter rejected: %s", reason)
	}
	for name, filter := range map[string]nostr.Filter{
		"missing p": {
			Kinds: []int{nostr.KindSimpleGroupPutUser},
			Tags:  nostr.TagMap{"h": []string{"club"}},
		},
		"foreign p": {
			Kinds: []int{nostr.KindSimpleGroupPutUser},
			Tags: nostr.TagMap{
				"h": []string{"club"},
				"p": []string{testOwner},
			},
		},
		"multi p": {
			Kinds: []int{nostr.KindSimpleGroupPutUser},
			Tags: nostr.TagMap{
				"h": []string{"club"},
				"p": []string{testMember, testOwner},
			},
		},
	} {
		if rejected, _ := g.rejectRead(context.Background(), filter); !rejected {
			t.Fatalf("non-member %s filter was not rejected", name)
		}
	}

	selfPut := &nostr.Event{
		ID:   "self",
		Kind: nostr.KindSimpleGroupPutUser,
		Tags: nostr.Tags{{"h", "club"}, {"p", testMember}},
	}
	foreignPut := &nostr.Event{
		ID:   "foreign",
		Kind: nostr.KindSimpleGroupPutUser,
		Tags: nostr.Tags{{"h", "club"}, {"p", testOwner}},
	}
	multiPut := &nostr.Event{
		ID:   "multi",
		Kind: nostr.KindSimpleGroupPutUser,
		Tags: nostr.Tags{{"h", "club"}, {"p", testMember}, {"p", testOwner}},
	}
	next := func(context.Context, nostr.Filter) (chan *nostr.Event, error) {
		ch := make(chan *nostr.Event, 3)
		ch <- selfPut
		ch <- foreignPut
		ch <- multiPut
		close(ch)
		return ch, nil
	}
	out, err := g.protectQuery(next)(context.Background(), selfFilter)
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for event := range out {
		got = append(got, event.ID)
	}
	if len(got) != 1 || got[0] != selfPut.ID {
		t.Fatalf("self-put query returned %v, want only %s", got, selfPut.ID)
	}

	ws := &khatru.WebSocket{AuthedPublicKey: testMember}
	if g.preventBroadcast(ws, selfPut) {
		t.Fatal("non-member must receive its exact self-put live transition")
	}
	if !g.preventBroadcast(ws, foreignPut) {
		t.Fatal("non-member received a foreign put-user transition")
	}
	if !g.preventBroadcast(ws, multiPut) {
		t.Fatal("multi-target put-user leaked a foreign identity through the self exception")
	}
}
