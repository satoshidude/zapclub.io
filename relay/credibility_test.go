package main

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/fiatjaf/eventstore/badger"
	"github.com/fiatjaf/khatru"
	"github.com/fiatjaf/relay29"
	"github.com/nbd-wtf/go-nostr"
	"github.com/puzpuzpuz/xsync/v3"
)

func TestAutoDJTrackDoesNotAffectCredibility(t *testing.T) {
	b := newCredibilityBoard(filepath.Join(t.TempDir(), "credibility.json"))
	c := &conductor{cred: b}
	c.settleTrack(context.Background(), "club", &condClub{
		pos: 1, startedAt: 1000, dj: "owner", playing: true, auto: true,
	}, trackCommunitySkipped)
	if len(b.By) != 0 {
		t.Fatalf("auto DJ changed credibility: %+v", b.By)
	}
}

func TestPublicAutoDJTrackAppearsOnlyInTrackHistory(t *testing.T) {
	b := newCredibilityBoard(filepath.Join(t.TempDir(), "credibility.json"))
	state := &relay29.State{Groups: xsync.NewMapOf[string, *relay29.Group]()}
	state.Groups.Store("club", state.NewGroup("club", "owner"))
	c := &conductor{
		cred:  b,
		state: state,
		moods: map[string]map[int]map[string]int{
			"club": {4: {"listener": 1}},
		},
	}
	c.settleTrack(context.Background(), "club", &condClub{
		pos: 4, startedAt: 4000, videoID: "auto-video", title: "Auto track",
		dj: "owner", playing: true, auto: true,
	}, trackPlayed)

	if len(b.By) != 0 {
		t.Fatalf("auto DJ changed owner credibility: %+v", b.By)
	}
	if len(b.TrackPerformances) != 1 {
		t.Fatalf("public Auto DJ performances = %+v; want one", b.TrackPerformances)
	}
	track := b.TrackPerformances[0]
	if !track.AutoDJ || track.Club != "club" || track.DJ != "owner" || track.Bangers != 1 {
		t.Fatalf("public Auto DJ performance = %+v", track)
	}
}

func TestNextAddressableTimestampAdvancesPastStoredSnapshot(t *testing.T) {
	db := &badger.BadgerBackend{Path: t.TempDir()}
	if err := db.Init(); err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	sk := nostr.GeneratePrivateKey()
	pubkey, err := nostr.GetPublicKey(sk)
	if err != nil {
		t.Fatal(err)
	}
	dTag := "zapclub:credibility:alice"
	existing := &nostr.Event{
		Kind: kindCredibility, CreatedAt: nostr.Now() + 5,
		Tags: nostr.Tags{{"d", dTag}},
	}
	if err := existing.Sign(sk); err != nil {
		t.Fatal(err)
	}
	if err := db.ReplaceEvent(context.Background(), existing); err != nil {
		t.Fatal(err)
	}
	c := &conductor{db: db, pub: pubkey}
	if got := c.nextAddressableTimestamp(context.Background(), kindCredibility, dTag); got != existing.CreatedAt+1 {
		t.Fatalf("next timestamp = %d, want %d", got, existing.CreatedAt+1)
	}
}

func TestCredibilityBoardScoresAndDeduplicatesTracks(t *testing.T) {
	b := newCredibilityBoard(filepath.Join(t.TempDir(), "credibility.json"))

	entry, ok := b.record("club", 1, 1000, "video-1", "Track one", "alice", 9, trackPlayed, true)
	if !ok || entry.Score != 5 || entry.Tracks != 1 || entry.Bangers != 5 || entry.Skipped != 0 {
		t.Fatalf("positive track = %+v, ok=%v; want score 5 with five capped bangers", entry, ok)
	}
	if _, ok := b.record("club", 1, 1000, "video-1", "Track one", "alice", 4, trackPlayed, true); ok {
		t.Fatal("same track was settled twice")
	}

	entry, ok = b.record("club", 2, 2000, "video-2", "Track two", "alice", 3, trackCommunitySkipped, true)
	if !ok || entry.Score != 4 || entry.Tracks != 2 || entry.Bangers != 8 || entry.Skipped != 1 {
		t.Fatalf("skipped track = %+v, ok=%v; want a single -1 penalty", entry, ok)
	}
}

func TestCredibilityBoardDiscardsManualOrBrokenTransitions(t *testing.T) {
	path := filepath.Join(t.TempDir(), "credibility.json")
	b := newCredibilityBoard(path)
	if entry, changed := b.record("club", 1, 1000, "video-1", "Track one", "alice", 5, trackDiscarded, true); changed || entry != (credibilityEntry{}) {
		t.Fatalf("discarded transition = %+v, changed=%v; it must not create rank data", entry, changed)
	}
	if b.By["alice"] != nil {
		t.Fatalf("discarded transition created credibility: %+v", b.By["alice"])
	}

	// The handled marker survives a restart, so stop() cannot later turn the same
	// manual/broken transition into a naturally completed song.
	reloaded := newCredibilityBoard(path)
	if _, changed := reloaded.record("club", 1, 1000, "video-1", "Track one", "alice", 5, trackPlayed, true); changed {
		t.Fatal("discarded transition was reclassified after reload")
	}
	entry, changed := reloaded.record("club", 2, 2000, "video-2", "Track two", "alice", 2, trackPlayed, true)
	if !changed || entry.Tracks != 1 || entry.Score != 2 || entry.Bangers != 2 {
		t.Fatalf("next naturally played track = %+v, changed=%v", entry, changed)
	}
}

func TestCredibilityBoardPersists(t *testing.T) {
	path := filepath.Join(t.TempDir(), "credibility.json")
	b := newCredibilityBoard(path)
	if _, ok := b.record("club", 7, 7000, "video-7", "Track seven", "alice", 2, trackPlayed, true); !ok {
		t.Fatal("first track was not recorded")
	}

	reloaded := newCredibilityBoard(path)
	entry := reloaded.By["alice"]
	if entry == nil || entry.Score != 2 || entry.Tracks != 1 || entry.Bangers != 2 {
		t.Fatalf("reloaded entry = %+v; want persisted score", entry)
	}
	if _, ok := reloaded.record("club", 7, 7000, "video-7", "Track seven", "alice", 2, trackPlayed, true); ok {
		t.Fatal("reloaded board did not preserve track deduplication")
	}
	if len(reloaded.TrackPerformances) != 1 {
		t.Fatalf("reloaded track performances = %+v; want one", reloaded.TrackPerformances)
	}
	track := reloaded.TrackPerformances[0]
	if track.Club != "club" || track.VideoID != "video-7" || track.Title != "Track seven" ||
		track.DJ != "alice" || track.Bangers != 2 || track.Skipped || track.AutoDJ || track.StartedAt != 7000 {
		t.Fatalf("reloaded track performance = %+v", track)
	}
}

func TestCredibilityBoardKeepsBoundedBestTrackPerformances(t *testing.T) {
	b := newCredibilityBoard(filepath.Join(t.TempDir(), "credibility.json"))
	for i := 0; i < credibilityTrackHistoryMax+5; i++ {
		bangers := i % (moodBangerMax + 1)
		if _, ok := b.record("club", i, int64(1000+i), "video", "Track", "alice", bangers, trackPlayed, true); !ok {
			t.Fatalf("performance %d was not recorded", i)
		}
	}
	if len(b.TrackPerformances) != credibilityTrackHistoryMax {
		t.Fatalf("track history len = %d, want %d", len(b.TrackPerformances), credibilityTrackHistoryMax)
	}
	for i := 1; i < len(b.TrackPerformances); i++ {
		previous, current := b.TrackPerformances[i-1], b.TrackPerformances[i]
		if previous.Bangers < current.Bangers ||
			(previous.Bangers == current.Bangers && previous.StartedAt < current.StartedAt) {
			t.Fatalf("track history is not ranked at %d: %+v then %+v", i, previous, current)
		}
	}
}

func TestCredibilityBoardDoesNotPublishPrivateClubPerformance(t *testing.T) {
	b := newCredibilityBoard(filepath.Join(t.TempDir(), "credibility.json"))
	entry, ok := b.record("private-club", 1, 1000, "video", "Private track", "alice", 5, trackPlayed, false)
	if !ok || entry.Tracks != 1 || entry.Bangers != 5 {
		t.Fatalf("private play must still affect the DJ aggregate: entry=%+v ok=%v", entry, ok)
	}
	if len(b.TrackPerformances) != 0 {
		t.Fatalf("private club leaked into public performance history: %+v", b.TrackPerformances)
	}
}

func TestRealDJTrackSettlesBeforeAutoDJTakesOver(t *testing.T) {
	db := &badger.BadgerBackend{Path: t.TempDir()}
	if err := db.Init(); err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	credibility := newCredibilityBoard(filepath.Join(t.TempDir(), "credibility.json"))
	c := newConductor(db, khatru.NewRelay(), nil, nostr.GeneratePrivateKey())
	c.cred = credibility
	c.clubs["club"] = &condClub{
		pos: 7, videoID: "real-track", dj: "alice", duration: 120,
		startedAt: 1000, lastBeat: 1000, playing: true, auto: false,
	}
	c.moods["club"] = map[int]map[string]int{7: {"listener": 2}}
	c.skipCounts["club"] = map[int]map[string]int{7: {
		"listener-a": 1,
		"listener-b": 1,
		"listener-c": 1,
	}}

	auto := &autoState{owner: "owner", tracks: []condTrack{{
		videoID: "auto-track", title: "Auto track", duration: 1, active: true,
	}}}
	c.driveAutoClub(context.Background(), "club", auto, 20_000)

	entry := credibility.By["alice"]
	if entry == nil || entry.Tracks != 1 || entry.Score != -1 || entry.Bangers != 2 || entry.Skipped != 1 {
		t.Fatalf("real track handoff = %+v; want one community-skipped settled track", entry)
	}
	if playing := c.clubs["club"]; !playing.playing || !playing.auto || playing.videoID != "auto-track" {
		t.Fatalf("Auto DJ did not take over: %+v", playing)
	}

	// A later Auto-DJ-to-Auto-DJ transition must not accrue credibility for its owner
	// or settle the departed DJ's track a second time.
	c.driveAutoClub(context.Background(), "club", auto, 22_000)
	if entry.Tracks != 1 || credibility.By["owner"] != nil {
		t.Fatalf("Auto DJ changed credibility after takeover: alice=%+v owner=%+v", entry, credibility.By["owner"])
	}
}
