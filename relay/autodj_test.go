package main

import "testing"

func TestAutoUpcomingTracksMatchesCurrentAndPreplannedCycles(t *testing.T) {
	tracks := []condTrack{
		{videoID: "track-a", title: "A"},
		{videoID: "track-b", title: "B"},
		{videoID: "track-c", title: "C"},
	}
	pb := &condClub{autoOrder: []int{2, 0, 1}, autoIdx: 1}

	got := autoUpcomingTracks(pb, tracks, 3)
	if len(got) != 3 {
		t.Fatalf("preview length = %d, want 3", len(got))
	}
	if got[0].videoID != "track-b" {
		t.Fatalf("first preview track = %q, want remaining track-b", got[0].videoID)
	}
	if len(pb.autoNextOrder) != len(tracks) {
		t.Fatalf("next cycle was not preplanned: %v", pb.autoNextOrder)
	}
	if got[1].videoID != tracks[pb.autoNextOrder[0]].videoID || got[2].videoID != tracks[pb.autoNextOrder[1]].videoID {
		t.Fatalf("preview %v does not match preplanned next cycle %v", got, pb.autoNextOrder)
	}

	advanceAutoOrder(pb, len(tracks))
	if pb.autoIdx != 2 || pb.autoOrder[2] != 1 {
		t.Fatalf("advance within current cycle changed the order: idx=%d order=%v", pb.autoIdx, pb.autoOrder)
	}
	planned := append([]int(nil), pb.autoNextOrder...)
	advanceAutoOrder(pb, len(tracks))
	if pb.autoIdx != 0 || len(pb.autoNextOrder) != 0 {
		t.Fatalf("cycle transition state = idx %d next %v", pb.autoIdx, pb.autoNextOrder)
	}
	for i := range planned {
		if pb.autoOrder[i] != planned[i] {
			t.Fatalf("played order %v differs from announced order %v", pb.autoOrder, planned)
		}
	}
}

func TestAutoUpcomingTracksSingleTrackAnnouncesItsLoop(t *testing.T) {
	tracks := []condTrack{{videoID: "only-track", title: "Only"}}
	pb := &condClub{autoOrder: []int{0}, autoIdx: 0}

	got := autoUpcomingTracks(pb, tracks, 6)
	if len(got) != 1 || got[0].videoID != "only-track" {
		t.Fatalf("single-track preview = %v, want the looping track", got)
	}
}
