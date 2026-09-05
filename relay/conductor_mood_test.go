package main

import (
	"context"
	"testing"

	"github.com/nbd-wtf/go-nostr"
)

func moodEvent(pubkey, vote string) *nostr.Event {
	return &nostr.Event{
		Kind:   kindMood,
		PubKey: pubkey,
		Tags:   nostr.Tags{{"h", "club"}, {"pos", "5"}, {"v", vote}},
	}
}

func TestMoodCountsCapAtScoreAndSkipThresholds(t *testing.T) {
	c := newConductor(nil, nil, nil, "")
	c.clubs["club"] = &condClub{pos: 5, playing: true}
	for i := 0; i < 9; i++ {
		c.observeMood(context.Background(), moodEvent("alice", "banger"))
	}
	for i := 0; i < 7; i++ {
		c.observeMood(context.Background(), moodEvent("alice", "skip"))
	}
	bangers, skips := c.moodCounts("club", 5)
	if bangers != moodBangerMax || skips != moodSkipThreshold {
		t.Fatalf("counts = %d/%d; want %d/%d", bangers, skips, moodBangerMax, moodSkipThreshold)
	}
}

func TestMoodIgnoresNonCurrentPosition(t *testing.T) {
	c := newConductor(nil, nil, nil, "")
	c.clubs["club"] = &condClub{pos: 6, playing: true}
	c.observeMood(context.Background(), moodEvent("alice", "banger"))
	bangers, skips := c.moodCounts("club", 5)
	if bangers != 0 || skips != 0 {
		t.Fatalf("stale position counted as %d/%d", bangers, skips)
	}
}
