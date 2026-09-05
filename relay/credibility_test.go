package main

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/fiatjaf/eventstore/badger"
	"github.com/nbd-wtf/go-nostr"
)

func TestAutoDJTrackDoesNotAffectCredibility(t *testing.T) {
	b := newCredibilityBoard(filepath.Join(t.TempDir(), "credibility.json"))
	c := &conductor{cred: b}
	c.settleTrack(context.Background(), "club", &condClub{
		pos: 1, startedAt: 1000, dj: "owner", playing: true, auto: true,
	}, true)
	if len(b.By) != 0 {
		t.Fatalf("auto DJ changed credibility: %+v", b.By)
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

	entry, ok := b.record("club", 1, 1000, "alice", 9, false)
	if !ok || entry.Score != 5 || entry.Tracks != 1 || entry.Bangers != 5 || entry.Skipped != 0 {
		t.Fatalf("positive track = %+v, ok=%v; want score 5 with five capped bangers", entry, ok)
	}
	if _, ok := b.record("club", 1, 1000, "alice", 4, false); ok {
		t.Fatal("same track was settled twice")
	}

	entry, ok = b.record("club", 2, 2000, "alice", 3, true)
	if !ok || entry.Score != 4 || entry.Tracks != 2 || entry.Bangers != 8 || entry.Skipped != 1 {
		t.Fatalf("skipped track = %+v, ok=%v; want a single -1 penalty", entry, ok)
	}
}

func TestCredibilityBoardPersists(t *testing.T) {
	path := filepath.Join(t.TempDir(), "credibility.json")
	b := newCredibilityBoard(path)
	if _, ok := b.record("club", 7, 7000, "alice", 2, false); !ok {
		t.Fatal("first track was not recorded")
	}

	reloaded := newCredibilityBoard(path)
	entry := reloaded.By["alice"]
	if entry == nil || entry.Score != 2 || entry.Tracks != 1 || entry.Bangers != 2 {
		t.Fatalf("reloaded entry = %+v; want persisted score", entry)
	}
	if _, ok := reloaded.record("club", 7, 7000, "alice", 2, false); ok {
		t.Fatal("reloaded board did not preserve track deduplication")
	}
}
