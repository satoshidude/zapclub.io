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
	c.clubs["club"] = &condClub{pos: 5, dj: "bob", playing: true}
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

func TestMoodRejectsCurrentDJsOwnVote(t *testing.T) {
	for _, auto := range []bool{false, true} {
		name := "real DJ"
		if auto {
			name = "Auto DJ owner"
		}
		t.Run(name, func(t *testing.T) {
			c := newConductor(nil, nil, nil, "")
			c.clubs["club"] = &condClub{pos: 5, dj: "alice", playing: true, auto: auto}

			rejected, reason := c.rejectMood(context.Background(), moodEvent("alice", "banger"))
			if !rejected || reason != "blocked: DJs cannot vote on their own track" {
				t.Fatalf("own vote rejected=%v reason=%q", rejected, reason)
			}
			if rejected, reason = c.rejectMood(context.Background(), moodEvent("bob", "banger")); rejected {
				t.Fatalf("another member's vote rejected: %q", reason)
			}
		})
	}
}

func TestMoodRejectsVotesWithoutAnAuthoritativeCurrentTrack(t *testing.T) {
	c := newConductor(nil, nil, nil, "")

	rejected, reason := c.rejectMood(context.Background(), moodEvent("alice", "skip"))
	if !rejected || reason != "blocked: Vibemeter reaction does not target the current track" {
		t.Fatalf("vote without current track rejected=%v reason=%q", rejected, reason)
	}
}

func TestMoodIgnoresCurrentDJsOwnVoteIfRejectChainIsBypassed(t *testing.T) {
	c := newConductor(nil, nil, nil, "")
	c.clubs["club"] = &condClub{pos: 5, dj: "alice", playing: true}
	c.observeMood(context.Background(), moodEvent("alice", "banger"))
	c.observeMood(context.Background(), moodEvent("alice", "skip"))

	bangers, skips := c.moodCounts("club", 5)
	if bangers != 0 || skips != 0 {
		t.Fatalf("own vote counted as %d/%d", bangers, skips)
	}
}
