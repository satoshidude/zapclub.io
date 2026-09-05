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
