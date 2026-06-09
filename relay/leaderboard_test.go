package main

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/nbd-wtf/go-nostr"
)

func bcast(sender, recipient, amount, bolt11 string) *nostr.Event {
	tags := nostr.Tags{{"h", "club"}, {"p", recipient}, {"amount", amount}}
	if bolt11 != "" {
		tags = append(tags, nostr.Tag{"bolt11", bolt11})
	}
	ev := &nostr.Event{Kind: kindZapBroadcast, PubKey: sender, Tags: tags}
	ev.ID = ev.GetID()
	return ev
}

func TestZapBoardAccumulatesAndRanks(t *testing.T) {
	b := newZapBoard(filepath.Join(t.TempDir(), "lb.json"))
	ctx := context.Background()
	// alice receives 100 (from S1) + 50 (from S2); bob receives 30 (from S1).
	b.observe(ctx, bcast("S1", "alice", "100", "inv1"))
	b.observe(ctx, bcast("S2", "alice", "50", "inv2"))
	b.observe(ctx, bcast("S1", "bob", "30", "inv3"))

	ae, total, ok := b.rankOf("alice")
	if !ok || total != 2 {
		t.Fatalf("alice: ok=%v total=%d want ok,2", ok, total)
	}
	if ae.Sats != 150 || ae.Zaps != 2 || ae.Zappers != 2 || ae.Rank != 1 {
		t.Errorf("alice entry = %+v; want sats150 zaps2 zappers2 rank1", ae)
	}
	be, _, _ := b.rankOf("bob")
	if be.Sats != 30 || be.Zappers != 1 || be.Rank != 2 {
		t.Errorf("bob entry = %+v; want sats30 zappers1 rank2", be)
	}
}

func TestZapBoardDedupAndSelfZapAndDistinctSenders(t *testing.T) {
	b := newZapBoard(filepath.Join(t.TempDir(), "lb.json"))
	ctx := context.Background()
	// same bolt11 twice → counted once
	b.observe(ctx, bcast("S1", "alice", "100", "dup"))
	b.observe(ctx, bcast("S1", "alice", "100", "dup"))
	// self-zap ignored
	b.observe(ctx, bcast("alice", "alice", "9999", "self"))
	// same sender again (new zap) → sats add, distinct senders stays 1
	b.observe(ctx, bcast("S1", "alice", "20", "inv2"))
	// zero / missing amount ignored
	b.observe(ctx, bcast("S2", "alice", "0", "zero"))

	e, _, ok := b.rankOf("alice")
	if !ok {
		t.Fatal("alice should be ranked")
	}
	if e.Sats != 120 {
		t.Errorf("sats = %d; want 120 (dup + self + zero excluded)", e.Sats)
	}
	if e.Zaps != 2 {
		t.Errorf("zaps = %d; want 2", e.Zaps)
	}
	if e.Zappers != 1 {
		t.Errorf("zappers = %d; want 1 (one distinct sender)", e.Zappers)
	}
}

func TestZapBoardPersists(t *testing.T) {
	path := filepath.Join(t.TempDir(), "lb.json")
	b := newZapBoard(path)
	b.observe(context.Background(), bcast("S1", "alice", "100", "inv1"))
	b.save()

	b2 := newZapBoard(path)
	e, total, ok := b2.rankOf("alice")
	if !ok || total != 1 || e.Sats != 100 || e.Zappers != 1 {
		t.Errorf("reloaded board: ok=%v total=%d entry=%+v; want 100 sats / 1 zapper", ok, total, e)
	}
}

func TestZapBoardTop(t *testing.T) {
	b := newZapBoard(filepath.Join(t.TempDir(), "lb.json"))
	ctx := context.Background()
	b.observe(ctx, bcast("S1", "alice", "100", "i1"))
	b.observe(ctx, bcast("S1", "bob", "300", "i2"))
	b.observe(ctx, bcast("S1", "carol", "200", "i3"))
	top, total := b.top(2)
	if total != 3 || len(top) != 2 {
		t.Fatalf("top: total=%d len=%d want 3,2", total, len(top))
	}
	if top[0].Pubkey != "bob" || top[0].Rank != 1 || top[1].Pubkey != "carol" || top[1].Rank != 2 {
		t.Errorf("top order = %+v; want bob#1, carol#2", top)
	}
}
