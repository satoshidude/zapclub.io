package main

import (
	"path/filepath"
	"testing"
	"time"
)

func TestBanStore(t *testing.T) {
	path := filepath.Join(t.TempDir(), "banned.json")
	b := newBanStore(path)

	if b.isBanned("pk1") {
		t.Fatal("fresh store should have no bans")
	}
	b.ban("pk1", "spam")
	if !b.isBanned("pk1") {
		t.Fatal("pk1 should be banned")
	}
	if got := b.list()["pk1"]; got != "spam" {
		t.Fatalf("reason = %q, want spam", got)
	}

	// Persists across reload (the list lives next to the DB and must survive restarts).
	b2 := newBanStore(path)
	if !b2.isBanned("pk1") {
		t.Fatal("ban should persist across reload")
	}

	b.unban("pk1")
	if b.isBanned("pk1") {
		t.Fatal("pk1 should be unbanned")
	}
	if newBanStore(path).isBanned("pk1") {
		t.Fatal("unban should persist across reload")
	}
}

func TestAdminNonceReplay(t *testing.T) {
	adminNonces.Range(func(k, _ any) bool { adminNonces.Delete(k); return true })

	if adminNonceSeen("evt-A") {
		t.Fatal("first use of a token must be allowed")
	}
	if !adminNonceSeen("evt-A") {
		t.Fatal("second use of the same token must be rejected (replay)")
	}
	if adminNonceSeen("evt-B") {
		t.Fatal("a different token must be allowed")
	}
}

func TestPruneAdminNonces(t *testing.T) {
	adminNonces.Range(func(k, _ any) bool { adminNonces.Delete(k); return true })
	// An already-expired entry should be pruned; a future one kept.
	adminNonces.Store("old", time.Now().Add(-time.Minute))
	adminNonces.Store("new", time.Now().Add(time.Minute))
	pruneAdminNonces()
	if _, ok := adminNonces.Load("old"); ok {
		t.Fatal("expired nonce should be pruned")
	}
	if _, ok := adminNonces.Load("new"); !ok {
		t.Fatal("unexpired nonce should be kept")
	}
}

func TestCapBuffer(t *testing.T) {
	cb := &capBuffer{cap: 8}
	n, _ := cb.Write([]byte("12345"))
	if n != 5 {
		t.Fatalf("Write reported %d, want 5 (must claim full write so the child never blocks)", n)
	}
	// Writing past the cap is accepted (reported written) but dropped beyond `cap`.
	n, _ = cb.Write([]byte("67890"))
	if n != 5 {
		t.Fatalf("Write reported %d, want 5", n)
	}
	if got := cb.buf.String(); got != "12345678" {
		t.Fatalf("buffer = %q, want %q (capped at 8)", got, "12345678")
	}
}
