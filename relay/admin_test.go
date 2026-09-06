package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestAdminMutationContextSurvivesRequestCancellation(t *testing.T) {
	requestCtx, cancelRequest := context.WithCancel(context.Background())
	cancelRequest()
	mutationCtx, cancelMutation := adminMutationContext(requestCtx)
	defer cancelMutation()
	select {
	case <-mutationCtx.Done():
		t.Fatalf("request cancellation leaked into durable admin mutation: %v", mutationCtx.Err())
	default:
	}
}

func TestBanStore(t *testing.T) {
	path := filepath.Join(t.TempDir(), "banned.json")
	b := newBanStore(path)

	if b.isBanned("pk1") {
		t.Fatal("fresh store should have no bans")
	}
	if err := b.ban("pk1", "spam"); err != nil {
		t.Fatal(err)
	}
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

	if err := b.unban("pk1"); err != nil {
		t.Fatal(err)
	}
	if b.isBanned("pk1") {
		t.Fatal("pk1 should be unbanned")
	}
	if newBanStore(path).isBanned("pk1") {
		t.Fatal("unban should persist across reload")
	}
}

func TestBanStorePersistenceFailureDoesNotChangeLiveState(t *testing.T) {
	missingParent := filepath.Join(t.TempDir(), "missing", "banned.json")
	b := newBanStore(missingParent)
	if err := b.ban("pk1", "spam"); err == nil {
		t.Fatal("ban should fail when its persistence directory is missing")
	}
	if b.isBanned("pk1") {
		t.Fatal("failed durable ban changed the live ban map")
	}

	validPath := filepath.Join(t.TempDir(), "banned.json")
	b = newBanStore(validPath)
	if err := b.ban("pk1", "spam"); err != nil {
		t.Fatal(err)
	}
	b.path = missingParent
	if err := b.unban("pk1"); err == nil {
		t.Fatal("unban should fail when persistence cannot be replaced")
	}
	if !b.isBanned("pk1") {
		t.Fatal("failed durable unban removed the live ban")
	}
	if !newBanStore(validPath).isBanned("pk1") {
		t.Fatal("failed durable unban changed the previously persisted ban")
	}
}

func TestAdminBanPersistenceFailureReturnsErrorWithoutRevocation(t *testing.T) {
	bans := newBanStore(filepath.Join(t.TempDir(), "missing", "banned.json"))
	revoked := false
	api := &adminAPI{bans: bans, onBan: func(string) { revoked = true }}
	req := httptest.NewRequest(http.MethodPost, "/admin/ban", strings.NewReader(
		`{"pubkey":"`+testMember+`","reason":"spam"}`,
	))
	response := httptest.NewRecorder()
	api.ban(response, req)
	if response.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", response.Code)
	}
	if bans.isBanned(testMember) || revoked {
		t.Fatal("failed durable ban must neither activate nor run revocation callbacks")
	}
}

func TestAdminUnbanPersistenceFailureKeepsBan(t *testing.T) {
	bans := newBanStore(filepath.Join(t.TempDir(), "banned.json"))
	if err := bans.ban(testMember, "spam"); err != nil {
		t.Fatal(err)
	}
	bans.path = filepath.Join(t.TempDir(), "missing", "banned.json")
	api := &adminAPI{bans: bans}
	req := httptest.NewRequest(http.MethodPost, "/admin/unban", strings.NewReader(
		`{"pubkey":"`+testMember+`"}`,
	))
	response := httptest.NewRecorder()
	api.unban(response, req)
	if response.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", response.Code)
	}
	if !bans.isBanned(testMember) {
		t.Fatal("failed durable unban must retain the active ban")
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
