package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

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

func nip98Request(t *testing.T, method, path, sk string, body string) *http.Request {
	t.Helper()
	ev := nostr.Event{
		Kind:      27235,
		CreatedAt: nostr.Timestamp(time.Now().Unix()),
		Tags: nostr.Tags{
			{"u", "https://relay.zapclub.io" + path},
			{"method", method},
		},
	}
	if err := ev.Sign(sk); err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(ev)
	if err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRequest(method, path, strings.NewReader(body))
	r.Header.Set("Authorization", "Nostr "+base64.StdEncoding.EncodeToString(raw))
	return r
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

func TestZapBoardLoadsLegacyTotalsWithoutInventingSenderAmounts(t *testing.T) {
	path := filepath.Join(t.TempDir(), "lb.json")
	legacy := `{"by":{"alice":{"sats":150,"zaps":2,"senders":{"sender-a":true,"sender-b":true}}}}`
	if err := os.WriteFile(path, []byte(legacy), 0o600); err != nil {
		t.Fatal(err)
	}

	received := newZapBoard(path).received("alice")
	if received.Total != 150 || received.Count != 2 || len(received.BySender) != 2 {
		t.Fatalf("legacy received = %+v; totals and senders must survive", received)
	}
	for _, sender := range received.BySender {
		if sender.Exact || sender.Sats != 0 || sender.Count != 0 {
			t.Fatalf("legacy sender = %+v; unknown historical split must not be invented", sender)
		}
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

func TestZapBoardKeepsSiteSenderBreakdown(t *testing.T) {
	b := newZapBoard(filepath.Join(t.TempDir(), "lb.json"))
	b.observe(context.Background(), bcast("sender-a", "alice", "100", "i1"))
	b.observe(context.Background(), bcast("sender-a", "alice", "20", "i2"))
	b.observe(context.Background(), bcast("sender-b", "alice", "50", "i3"))

	received := b.received("alice")
	if received.Total != 170 || received.Count != 3 || len(received.BySender) != 2 {
		t.Fatalf("received = %+v; want 170 sats / 3 zaps / 2 senders", received)
	}
	if got := received.BySender[0]; got.Sender != "sender-a" || got.Sats != 120 || got.Count != 2 || !got.Exact {
		t.Errorf("first sender = %+v; want sender-a, 120 sats, 2 zaps, exact", got)
	}
}

func TestZapBoardDeduplicatesHTTPRecordAndClubBroadcast(t *testing.T) {
	b := newZapBoard(filepath.Join(t.TempDir(), "lb.json"))
	b.record("sender", "alice", 100, "bolt11:lnbc_invoice")
	b.observe(context.Background(), bcast("sender", "alice", "100", "lnbc_invoice"))

	e, _, ok := b.rankOf("alice")
	if !ok || e.Sats != 100 || e.Zaps != 1 {
		t.Fatalf("entry = %+v, ok=%v; duplicated invoice must count once", e, ok)
	}
}

func TestZapTagRecognizesValuelessAnonymousMarker(t *testing.T) {
	if !hasTagValue(nostr.Tags{{"anon"}}, "anon", "") {
		t.Fatal("valueless NIP-57 anon tag must be recognized")
	}
}

func TestZapHTTPAttributesSignedRequestAndProtectsReceivedList(t *testing.T) {
	adminNonces.Range(func(key, _ any) bool { adminNonces.Delete(key); return true })
	b := newZapBoard(filepath.Join(t.TempDir(), "lb.json"))
	senderSK := nostr.GeneratePrivateKey()
	sender, _ := nostr.GetPublicKey(senderSK)
	recipientSK := nostr.GeneratePrivateKey()
	recipient, _ := nostr.GetPublicKey(recipientSK)

	zapRequest := nostr.Event{
		Kind:      nostr.KindZapRequest,
		CreatedAt: nostr.Timestamp(time.Now().Unix()),
		Tags: nostr.Tags{
			{"p", recipient},
			{"amount", "210000"},
			{"client", "zapclub.io"},
		},
	}
	if err := zapRequest.Sign(senderSK); err != nil {
		t.Fatal(err)
	}
	body, err := json.Marshal(map[string]any{"request": zapRequest, "invoice": "lnbc_test_invoice"})
	if err != nil {
		t.Fatal(err)
	}
	post := httptest.NewRequest(http.MethodPost, "/zaps", strings.NewReader(string(body)))
	postRecorder := httptest.NewRecorder()
	b.handleZaps(postRecorder, post)
	if postRecorder.Code != http.StatusOK {
		t.Fatalf("POST /zaps = %d: %s", postRecorder.Code, postRecorder.Body.String())
	}

	get := nip98Request(t, http.MethodGet, "/zaps/received", recipientSK, "")
	getRecorder := httptest.NewRecorder()
	b.handleZaps(getRecorder, get)
	if getRecorder.Code != http.StatusOK {
		t.Fatalf("GET /zaps/received = %d: %s", getRecorder.Code, getRecorder.Body.String())
	}
	var received receivedZaps
	if err := json.Unmarshal(getRecorder.Body.Bytes(), &received); err != nil {
		t.Fatal(err)
	}
	if received.Total != 210 || len(received.BySender) != 1 || received.BySender[0].Sender != sender {
		t.Fatalf("received = %+v; sender must come from signed zap request", received)
	}

	unauthorized := httptest.NewRequest(http.MethodGet, "/zaps/received", nil)
	unauthorizedRecorder := httptest.NewRecorder()
	b.handleZaps(unauthorizedRecorder, unauthorized)
	if unauthorizedRecorder.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated received list = %d; want 401", unauthorizedRecorder.Code)
	}
}
