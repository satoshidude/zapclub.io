package main

import (
	"path/filepath"
	"testing"

	"github.com/nbd-wtf/go-nostr"
)

func TestListenerStats(t *testing.T) {
	s := newListenerStats(filepath.Join(t.TempDir(), "listeners.json"))
	const base = int64(1_700_000_000_000)

	// Two listeners beat in club A, one in club B, all within the first bucket.
	s.record("A", "pk1", base)
	s.record("A", "pk2", base+1000)
	s.record("B", "pk3", base+2000)

	snap := s.snapshot(base + 3000)
	byID := map[string]clubListeners{}
	for _, c := range snap.Clubs {
		byID[c.ID] = c
	}
	if got := len(byID["A"].Live); got != 2 {
		t.Errorf("club A live = %d, want 2", got)
	}
	if got := len(byID["B"].Live); got != 1 {
		t.Errorf("club B live = %d, want 1", got)
	}
	// Open-bucket tail reflects the in-flight count.
	a := byID["A"].Series
	if a[len(a)-1].N != 2 {
		t.Errorf("club A open-bucket count = %d, want 2", a[len(a)-1].N)
	}
	if len(byID["A"].Seen) != 2 {
		t.Errorf("club A seen = %d, want 2", len(byID["A"].Seen))
	}

	// Advance past the live window but stay in 24h: nobody is live, but the finalized
	// bucket keeps its count and the seen-spans persist.
	snap = s.snapshot(base + 2*listenBucketMs)
	for _, c := range snap.Clubs {
		if len(c.Live) != 0 {
			t.Errorf("club %s still live after the online window: %d", c.ID, len(c.Live))
		}
	}
	a = byID2(snap, "A").Series
	finalized := false
	for _, x := range a {
		if x.T == base-base%listenBucketMs && x.N == 2 {
			finalized = true
		}
	}
	if !finalized {
		t.Errorf("club A first bucket not finalized with count 2: %+v", a)
	}
	if len(byID2(snap, "A").Seen) != 2 {
		t.Errorf("club A seen spans dropped too early")
	}

	// Beyond 24h: the window has fully rolled off → club leaves the tracker.
	snap = s.snapshot(base + listenWindowMs + 2*listenBucketMs)
	if len(snap.Clubs) != 0 {
		t.Errorf("expected all clubs aged out after 24h, got %d", len(snap.Clubs))
	}
}

func TestListenerLiveBroadcastsChangesAndExpiry(t *testing.T) {
	s := newListenerStats(filepath.Join(t.TempDir(), "listeners.json"))
	const base = int64(1_700_000_000_000)
	type update struct {
		club  string
		count int
	}
	var updates []update
	s.setCountPublisher(func(club string, count int, _ int64) {
		updates = append(updates, update{club: club, count: count})
	})

	s.record("A", "session-1", base)
	s.broadcastLive(base)
	s.broadcastLive(base + 1000) // unchanged and inside refresh cadence
	s.record("A", "session-2", base+2000)
	s.broadcastLive(base + 2000)
	s.remove("A", "session-1")
	s.broadcastLive(base + 3000)
	s.broadcastLive(base + 3000 + listenOnlineMs) // remaining session expires

	want := []update{{"A", 1}, {"A", 2}, {"A", 1}, {"A", 0}}
	if len(updates) != len(want) {
		t.Fatalf("updates = %+v, want %+v", updates, want)
	}
	for i := range want {
		if updates[i] != want[i] {
			t.Fatalf("update %d = %+v, want %+v", i, updates[i], want[i])
		}
	}
}

func TestValidListenerBeat(t *testing.T) {
	const now = nostr.Timestamp(1_700_000_000)
	valid := &nostr.Event{
		Kind:      kindListenerBeat,
		CreatedAt: now,
		Tags:      nostr.Tags{{"h", "club"}, {"state", "on"}},
	}
	if !validListenerBeat(valid, now) {
		t.Fatal("valid anonymous listener heartbeat was rejected")
	}

	cases := []*nostr.Event{
		{Kind: kindListenerBeat, CreatedAt: now - 61, Tags: valid.Tags},
		{Kind: kindListenerBeat, CreatedAt: now, Tags: nostr.Tags{{"h", "club"}}},
		{Kind: kindListenerBeat, CreatedAt: now, Tags: nostr.Tags{{"h", "club"}, {"state", "maybe"}}},
		{Kind: kindListenerBeat, CreatedAt: now, Tags: nostr.Tags{{"h", "club"}, {"state", "on"}, {"p", "tracking"}}},
		{Kind: kindListenerBeat, CreatedAt: now, Tags: valid.Tags, Content: "identity"},
	}
	for i, event := range cases {
		if validListenerBeat(event, now) {
			t.Fatalf("invalid heartbeat %d was accepted", i)
		}
	}
}

func TestRawListenerBeatsAreNeverBroadcast(t *testing.T) {
	if !preventListenerBeatBroadcast(nil, &nostr.Event{Kind: kindListenerBeat}) {
		t.Fatal("raw anonymous listener heartbeat must stay server-side")
	}
	if preventListenerBeatBroadcast(nil, &nostr.Event{Kind: kindListenerCount}) {
		t.Fatal("relay-authored listener aggregate must remain broadcastable")
	}
}

func byID2(snap listenersResp, id string) clubListeners {
	for _, c := range snap.Clubs {
		if c.ID == id {
			return c
		}
	}
	return clubListeners{}
}
