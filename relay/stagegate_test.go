package main

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/nbd-wtf/go-nostr"
)

func TestOrderAndCapDJsKeepsFirstThreeSlots(t *testing.T) {
	got := orderAndCapDJs([]condDJ{
		{pubkey: "d", since: 4},
		{pubkey: "b", since: 2},
		{pubkey: "a", since: 1},
		{pubkey: "c", since: 3},
	})
	if len(got) != 3 {
		t.Fatalf("stage size = %d, want 3", len(got))
	}
	for i, want := range []string{"a", "b", "c"} {
		if got[i].pubkey != want {
			t.Fatalf("slot %d = %q, want %q", i, got[i].pubkey, want)
		}
	}
}

func TestStageGateReservesThreeConcurrentSlotsAtomically(t *testing.T) {
	g := &stageGate{countFn: func(_ string, _ string) (int, bool) { return 0, false }}
	const attempts = 12
	var wg sync.WaitGroup
	results := make(chan bool, attempts)
	for i := 0; i < attempts; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			evt := &nostr.Event{
				Kind: kindStage, PubKey: string(rune('a' + i)), Content: "on",
				Tags: nostr.Tags{{"h", "club"}},
			}
			rejected, _ := g.reject(context.Background(), evt)
			results <- rejected
		}(i)
	}
	wg.Wait()
	close(results)
	accepted := 0
	for rejected := range results {
		if !rejected {
			accepted++
		}
	}
	if accepted != condMaxDJs {
		t.Fatalf("accepted %d concurrent joins, want exactly %d", accepted, condMaxDJs)
	}
}

func TestCountActiveOtherDJsRejectsLegacyFourthSlot(t *testing.T) {
	now := time.Now().UnixMilli()
	c := &conductor{
		stageIdx: map[string]map[string]stageEntry{
			"club": {
				"a": {since: 1, lastSeen: now, on: true},
				"b": {since: 2, lastSeen: now, on: true},
				"c": {since: 3, lastSeen: now, on: true},
				"d": {since: 4, lastSeen: now, on: true},
			},
		},
		kickIdx: map[string]map[string]int64{},
	}

	if active, onStage := c.countActiveOtherDJs("club", "a"); active != 2 || !onStage {
		t.Fatalf("first slot: active=%d onStage=%v, want 2,true", active, onStage)
	}
	if active, onStage := c.countActiveOtherDJs("club", "d"); active != 3 || onStage {
		t.Fatalf("legacy fourth slot: active=%d onStage=%v, want 3,false", active, onStage)
	}
}

func TestStageGateBlocksFourthDJButAllowsHeartbeat(t *testing.T) {
	g := &stageGate{countFn: func(_ string, sender string) (int, bool) {
		if sender == "existing" {
			return 2, true
		}
		return 3, false
	}}
	event := func(pubkey string) *nostr.Event {
		return &nostr.Event{Kind: kindStage, PubKey: pubkey, Content: "on", Tags: nostr.Tags{{"h", "club"}}}
	}

	if reject, reason := g.reject(context.Background(), event("existing")); reject {
		t.Fatalf("existing DJ heartbeat rejected: %s", reason)
	}
	if reject, reason := g.reject(context.Background(), event("fourth")); !reject || reason != "restricted: stage is full" {
		t.Fatalf("fourth DJ: reject=%v reason=%q", reject, reason)
	}
}

func TestStageGateCountsActiveAutoDJAsOneSlot(t *testing.T) {
	g := &stageGate{
		countFn:      func(_ string, _ string) (int, bool) { return 2, false },
		autoActiveFn: func(_ string) bool { return true },
	}
	evt := &nostr.Event{Kind: kindStage, PubKey: "third-real", Content: "on", Tags: nostr.Tags{{"h", "club"}}}
	if reject, reason := g.reject(context.Background(), evt); !reject || reason != "restricted: stage is full" {
		t.Fatalf("third real DJ with active Auto DJ: reject=%v reason=%q", reject, reason)
	}
}

func TestStageGateSerializesAutoDJArmWithRealDJJoins(t *testing.T) {
	autoActive := false
	g := &stageGate{
		countFn:      func(_ string, _ string) (int, bool) { return 2, false },
		autoActiveFn: func(_ string) bool { return autoActive },
	}
	auto := &nostr.Event{
		Kind: kindAutoDJ, PubKey: "owner", Content: "playlist",
		Tags: nostr.Tags{{"h", "club"}, {"status", "armed"}, {"track", "yt:one", "One", "120"}},
	}
	if reject, reason := g.reject(context.Background(), auto); reject {
		t.Fatalf("Auto DJ arm into final slot rejected: %s", reason)
	}
	join := &nostr.Event{Kind: kindStage, PubKey: "third-real", Content: "on", Tags: nostr.Tags{{"h", "club"}}}
	if reject, reason := g.reject(context.Background(), join); !reject || reason != "restricted: stage is full" {
		t.Fatalf("join racing reserved Auto DJ slot: reject=%v reason=%q", reject, reason)
	}
	g.observe(context.Background(), auto)
	autoActive = true
	if reject, reason := g.reject(context.Background(), auto); reject {
		t.Fatalf("active Auto DJ config replacement rejected: %s", reason)
	}
}

func TestStageGateRejectsAutoDJArmWhenRealStageIsFull(t *testing.T) {
	g := &stageGate{
		countFn:      func(_ string, _ string) (int, bool) { return 3, false },
		autoActiveFn: func(_ string) bool { return false },
	}
	auto := &nostr.Event{
		Kind: kindAutoDJ, PubKey: "owner", Content: "playlist",
		Tags: nostr.Tags{{"h", "club"}, {"status", "armed"}, {"track", "yt:one", "One", "120"}},
	}
	if reject, reason := g.reject(context.Background(), auto); !reject || reason != "restricted: stage is full" {
		t.Fatalf("Auto DJ arm on full stage: reject=%v reason=%q", reject, reason)
	}
}

func TestStageGateUsesSessionEffectivePrincipal(t *testing.T) {
	principal, _ := nostr.GetPublicKey(nostr.GeneratePrivateKey())
	var countedAs string
	g := &stageGate{countFn: func(_ string, sender string) (int, bool) {
		countedAs = sender
		return 2, true
	}}
	event := signedSessionTestEvent(t, principal, kindStage, nostr.Now(), nil)
	if rejected, reason := g.reject(context.Background(), event); rejected {
		t.Fatalf("existing principal's session-key heartbeat rejected: %s", reason)
	}
	if countedAs != principal {
		t.Fatalf("stage gate counted %q, want principal %q", countedAs, principal)
	}
}

func TestStageGateOnlyReservingEventCanReleasePendingJoin(t *testing.T) {
	g := &stageGate{countFn: func(_ string, _ string) (int, bool) { return 2, false }}
	event := func(id, pubkey, content string) *nostr.Event {
		return &nostr.Event{
			ID: id, Kind: kindStage, PubKey: pubkey, Content: content,
			Tags: nostr.Tags{{"h", "club"}},
		}
	}
	joinA := event("join-a", "a", "on")
	if rejected, reason := g.reject(context.Background(), joinA); rejected {
		t.Fatalf("initial final-slot join rejected: %s", reason)
	}

	offA := event("off-a", "a", "off")
	if rejected, reason := g.reject(context.Background(), offA); rejected {
		t.Fatalf("overlapping off event rejected: %s", reason)
	}
	g.observe(context.Background(), offA)
	joinB := event("join-b", "b", "on")
	if rejected, reason := g.reject(context.Background(), joinB); !rejected || reason != "restricted: stage is full" {
		t.Fatalf("off event released another event's reservation: reject=%v reason=%q", rejected, reason)
	}

	duplicateA := event("join-a-duplicate", "a", "on")
	if rejected, reason := g.reject(context.Background(), duplicateA); rejected {
		t.Fatalf("duplicate in-flight join rejected: %s", reason)
	}
	g.observe(context.Background(), duplicateA)
	if rejected, reason := g.reject(context.Background(), joinB); !rejected || reason != "restricted: stage is full" {
		t.Fatalf("duplicate join released another event's reservation: reject=%v reason=%q", rejected, reason)
	}

	g.observe(context.Background(), joinA)
	if rejected, reason := g.reject(context.Background(), joinB); rejected {
		t.Fatalf("reserving event did not release final slot: %s", reason)
	}
}

func TestStageGateOnlyReservingEventCanReleasePendingAutoDJArm(t *testing.T) {
	g := &stageGate{
		countFn:      func(_ string, _ string) (int, bool) { return 2, false },
		autoActiveFn: func(string) bool { return false },
	}
	auto := func(id, status string, tracks bool) *nostr.Event {
		tags := nostr.Tags{{"h", "club"}, {"status", status}}
		if tracks {
			tags = append(tags, nostr.Tag{"track", "yt:one", "One", "120"})
		}
		return &nostr.Event{ID: id, Kind: kindAutoDJ, PubKey: "owner", Tags: tags, Content: "playlist"}
	}
	arm := auto("arm", "armed", true)
	if rejected, reason := g.reject(context.Background(), arm); rejected {
		t.Fatalf("initial final-slot Auto DJ arm rejected: %s", reason)
	}

	disarm := auto("disarm", "off", false)
	if rejected, reason := g.reject(context.Background(), disarm); rejected {
		t.Fatalf("overlapping disarm rejected: %s", reason)
	}
	g.observe(context.Background(), disarm)
	join := &nostr.Event{
		ID: "join", Kind: kindStage, PubKey: "dj", Content: "on",
		Tags: nostr.Tags{{"h", "club"}},
	}
	if rejected, reason := g.reject(context.Background(), join); !rejected || reason != "restricted: stage is full" {
		t.Fatalf("disarm released another event's reservation: reject=%v reason=%q", rejected, reason)
	}

	replacement := auto("replacement", "armed", true)
	if rejected, reason := g.reject(context.Background(), replacement); rejected {
		t.Fatalf("duplicate in-flight Auto DJ arm rejected: %s", reason)
	}
	g.observe(context.Background(), replacement)
	if rejected, reason := g.reject(context.Background(), join); !rejected || reason != "restricted: stage is full" {
		t.Fatalf("config replacement released another event's reservation: reject=%v reason=%q", rejected, reason)
	}

	g.observe(context.Background(), arm)
	if rejected, reason := g.reject(context.Background(), join); rejected {
		t.Fatalf("reserving Auto DJ event did not release final slot: %s", reason)
	}
}
