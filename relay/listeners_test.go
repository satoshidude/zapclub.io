package main

import "testing"

func TestListenerStats(t *testing.T) {
	s := newListenerStats("/tmp/zapclub-listeners-test.json")
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

func byID2(snap listenersResp, id string) clubListeners {
	for _, c := range snap.Clubs {
		if c.ID == id {
			return c
		}
	}
	return clubListeners{}
}
