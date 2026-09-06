package main

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/fiatjaf/eventstore/badger"
	"github.com/nbd-wtf/go-nostr"
)

func signedSessionTestEvent(t *testing.T, principal string, kind int, createdAt nostr.Timestamp, extra nostr.Tags) *nostr.Event {
	t.Helper()
	event := &nostr.Event{
		Kind:      kind,
		CreatedAt: createdAt,
		Tags: append(nostr.Tags{
			{"h", "club"},
			{"p", principal},
			{"client", sessionEventMarker},
		}, extra...),
		Content: "",
	}
	if err := event.Sign(nostr.GeneratePrivateKey()); err != nil {
		t.Fatal(err)
	}
	return event
}

func acceptingSessionPolicy(now time.Time, authed string) *sessionEventPolicy {
	policy := newSessionEventPolicy()
	policy.now = func() time.Time { return now }
	policy.getAuthed = func(context.Context) string { return authed }
	policy.setAuthorizers(
		func(groupID, pubkey string) bool { return groupID == "club" && pubkey == authed },
		func(string) bool { return false },
	)
	return policy
}

func TestSessionPolicyAcceptsSignedPresenceAndStage(t *testing.T) {
	now := time.Unix(1_800_000_000, 0)
	principal, err := nostr.GetPublicKey(nostr.GeneratePrivateKey())
	if err != nil {
		t.Fatal(err)
	}
	for _, kind := range []int{kindPresence, kindStage} {
		policy := acceptingSessionPolicy(now, principal)
		event := signedSessionTestEvent(t, principal, kind, nostr.Timestamp(now.Unix()), nil)
		if valid, err := event.CheckSignature(); !valid || err != nil {
			t.Fatalf("kind %d session signature invalid: valid=%v err=%v", kind, valid, err)
		}
		if rejected, reason := policy.reject(context.Background(), event); rejected {
			t.Fatalf("kind %d rejected: %s", kind, reason)
		}
		if got := effectiveEventPubKey(event); got != principal {
			t.Fatalf("kind %d effective pubkey = %q, want %q", kind, got, principal)
		}
	}
}

func TestSessionPolicyRejectsInvalidAuthorityAndFreshness(t *testing.T) {
	now := time.Unix(1_800_000_000, 0)
	principal, _ := nostr.GetPublicKey(nostr.GeneratePrivateKey())
	other, _ := nostr.GetPublicKey(nostr.GeneratePrivateKey())

	tests := []struct {
		name       string
		kind       int
		createdAt  nostr.Timestamp
		authed     string
		extra      nostr.Tags
		removeH    bool
		clientTail bool
		member     bool
		banned     bool
		wantReason string
	}{
		{name: "unauthenticated", kind: kindPresence, createdAt: nostr.Timestamp(now.Unix()), member: true, wantReason: "auth-required"},
		{name: "wrong p", kind: kindPresence, createdAt: nostr.Timestamp(now.Unix()), authed: other, member: true, wantReason: "does not match"},
		{name: "not a member", kind: kindPresence, createdAt: nostr.Timestamp(now.Unix()), authed: principal, wantReason: "current club members"},
		{name: "banned", kind: kindStage, createdAt: nostr.Timestamp(now.Unix()), authed: principal, member: true, banned: true, wantReason: "banned"},
		{name: "too old", kind: kindPresence, createdAt: nostr.Timestamp(now.Add(-61 * time.Second).Unix()), authed: principal, member: true, wantReason: "too old"},
		{name: "too far future", kind: kindPresence, createdAt: nostr.Timestamp(now.Add(31 * time.Second).Unix()), authed: principal, member: true, wantReason: "future"},
		{name: "duplicate p", kind: kindStage, createdAt: nostr.Timestamp(now.Unix()), authed: principal, member: true, extra: nostr.Tags{{"p", principal}}, wantReason: "malformed"},
		{name: "non-exact client marker", kind: kindStage, createdAt: nostr.Timestamp(now.Unix()), authed: principal, member: true, clientTail: true, wantReason: "malformed"},
		{name: "missing h", kind: kindStage, createdAt: nostr.Timestamp(now.Unix()), authed: principal, member: true, removeH: true, wantReason: "exactly one h"},
		{name: "unsupported kind", kind: kindQueue, createdAt: nostr.Timestamp(now.Unix()), authed: principal, member: true, wantReason: "only presence and stage"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			policy := newSessionEventPolicy()
			policy.now = func() time.Time { return now }
			policy.getAuthed = func(context.Context) string { return test.authed }
			policy.setAuthorizers(
				func(groupID, pubkey string) bool { return test.member && groupID == "club" && pubkey == principal },
				func(pubkey string) bool { return test.banned && pubkey == principal },
			)
			event := signedSessionTestEvent(t, principal, test.kind, test.createdAt, test.extra)
			if test.removeH {
				event.Tags = event.Tags[1:]
				if err := event.Sign(nostr.GeneratePrivateKey()); err != nil {
					t.Fatal(err)
				}
			}
			if test.clientTail {
				event.Tags[2] = nostr.Tag{"client", sessionEventMarker, "unexpected"}
				if err := event.Sign(nostr.GeneratePrivateKey()); err != nil {
					t.Fatal(err)
				}
			}
			rejected, reason := policy.reject(context.Background(), event)
			if !rejected || !strings.Contains(reason, test.wantReason) {
				t.Fatalf("reject=%v reason=%q, want reason containing %q", rejected, reason, test.wantReason)
			}
		})
	}
}

func TestSessionPolicyRejectsReplay(t *testing.T) {
	now := time.Unix(1_800_000_000, 0)
	principal, _ := nostr.GetPublicKey(nostr.GeneratePrivateKey())
	policy := acceptingSessionPolicy(now, principal)
	event := signedSessionTestEvent(t, principal, kindPresence, nostr.Timestamp(now.Unix()), nil)
	if rejected, reason := policy.reject(context.Background(), event); rejected {
		t.Fatalf("first send rejected: %s", reason)
	}
	if rejected, reason := policy.rejectReplay(context.Background(), event); rejected {
		t.Fatalf("first replay reservation rejected: %s", reason)
	}
	if rejected, reason := policy.rejectReplay(context.Background(), event); !rejected || !strings.Contains(reason, "already accepted") {
		t.Fatalf("replay reject=%v reason=%q", rejected, reason)
	}
}

func TestLaterIPRateRejectsDoNotConsumeReplayState(t *testing.T) {
	now := time.Unix(1_800_000_000, 0)
	principal, _ := nostr.GetPublicKey(nostr.GeneratePrivateKey())
	policy := acceptingSessionPolicy(now, principal)
	limiter := newIPLimiter(1, 0)
	rejectIP := func(context.Context, *nostr.Event) (bool, string) {
		return !limiter.allow("test-ip"), "rate-limited"
	}
	run := func(event *nostr.Event) (bool, string) {
		for _, reject := range []func(context.Context, *nostr.Event) (bool, string){
			policy.reject,
			rejectIP,
			policy.rejectReplay,
		} {
			if rejected, reason := reject(context.Background(), event); rejected {
				return true, reason
			}
		}
		return false, ""
	}
	first := signedSessionTestEvent(t, principal, kindPresence, nostr.Timestamp(now.Unix()), nil)
	if rejected, reason := run(first); rejected {
		t.Fatalf("first event rejected: %s", reason)
	}
	// More than the production IP burst must not add a single later-rejected id.
	for i := 0; i < 700; i++ {
		event := signedSessionTestEvent(t, principal, kindPresence, nostr.Timestamp(now.Unix()), nil)
		if rejected, reason := run(event); !rejected || !strings.Contains(reason, "rate-limited") {
			t.Fatalf("event %d reject=%v reason=%q", i, rejected, reason)
		}
	}
	policy.mu.Lock()
	seen := len(policy.replayByID)
	queued := policy.replay.Len()
	policy.mu.Unlock()
	if seen != 1 || queued != 1 {
		t.Fatalf("later-rejected event grew replay state to map=%d queue=%d, want 1,1", seen, queued)
	}
}

func TestConcurrentSessionReplayHasSingleWinner(t *testing.T) {
	now := time.Unix(1_800_000_000, 0)
	principal, _ := nostr.GetPublicKey(nostr.GeneratePrivateKey())
	policy := acceptingSessionPolicy(now, principal)
	event := signedSessionTestEvent(t, principal, kindPresence, nostr.Timestamp(now.Unix()), nil)
	if rejected, reason := policy.reject(context.Background(), event); rejected {
		t.Fatalf("validation rejected: %s", reason)
	}

	const attempts = 32
	results := make(chan bool, attempts)
	var wg sync.WaitGroup
	for i := 0; i < attempts; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			rejected, _ := policy.rejectReplay(context.Background(), event)
			results <- rejected
		}()
	}
	wg.Wait()
	close(results)
	accepted := 0
	for rejected := range results {
		if !rejected {
			accepted++
		}
	}
	if accepted != 1 {
		t.Fatalf("concurrent identical event accepted %d times, want exactly one", accepted)
	}
}

func TestStageRejectRollsBackReplayReservation(t *testing.T) {
	now := time.Unix(1_800_000_000, 0)
	principal, _ := nostr.GetPublicKey(nostr.GeneratePrivateKey())
	policy := acceptingSessionPolicy(now, principal)
	full := true
	stage := &stageGate{countFn: func(_ string, _ string) (int, bool) {
		if full {
			return condMaxDJs, false
		}
		return 0, false
	}}
	final := finalSessionReplayAndStageGate(policy, stage)
	event := signedSessionTestEvent(t, principal, kindStage, nostr.Timestamp(now.Unix()), nil)
	if rejected, reason := policy.reject(context.Background(), event); rejected {
		t.Fatalf("validation rejected: %s", reason)
	}
	if rejected, reason := final(context.Background(), event); !rejected || !strings.Contains(reason, "stage is full") {
		t.Fatalf("full stage reject=%v reason=%q", rejected, reason)
	}
	policy.mu.Lock()
	seenAfterReject := len(policy.replayByID)
	queuedAfterReject := policy.replay.Len()
	policy.mu.Unlock()
	if seenAfterReject != 0 || queuedAfterReject != 0 {
		t.Fatalf("stage-rejected event retained map=%d queue=%d replay entries", seenAfterReject, queuedAfterReject)
	}

	full = false
	if rejected, reason := final(context.Background(), event); rejected {
		t.Fatalf("same event could not be retried after capacity became free: %s", reason)
	}
}

func TestReplayCapacityRejectCannotReserveStageSlot(t *testing.T) {
	now := time.Unix(1_800_000_000, 0)
	principal, _ := nostr.GetPublicKey(nostr.GeneratePrivateKey())
	policy := acceptingSessionPolicy(now, principal)
	policy.replayMax = 4
	policy.mu.Lock()
	for i := 0; i < policy.replayMax; i++ {
		policy.addReplayLocked(string(rune(i+1)), now.Add(sessionEventReplayWindow))
	}
	policy.mu.Unlock()
	stage := &stageGate{countFn: func(_ string, _ string) (int, bool) { return 0, false }}
	final := finalSessionReplayAndStageGate(policy, stage)
	event := signedSessionTestEvent(t, principal, kindStage, nostr.Timestamp(now.Unix()), nil)
	if rejected, reason := final(context.Background(), event); !rejected || !strings.Contains(reason, "replay window is full") {
		t.Fatalf("full replay cache reject=%v reason=%q", rejected, reason)
	}
	stage.mu.Lock()
	pending := len(stage.pending)
	stage.mu.Unlock()
	if pending != 0 {
		t.Fatalf("replay-capacity rejection left %d stage reservations", pending)
	}
}

func TestStageRejectRollbacksKeepReplayQueueBounded(t *testing.T) {
	now := time.Unix(1_800_000_000, 0)
	principal, _ := nostr.GetPublicKey(nostr.GeneratePrivateKey())
	policy := acceptingSessionPolicy(now, principal)
	policy.replayMax = 8
	full := true
	stage := &stageGate{countFn: func(_ string, _ string) (int, bool) {
		if full {
			return condMaxDJs, false
		}
		return 0, false
	}}
	final := finalSessionReplayAndStageGate(policy, stage)
	for i := 0; i < policy.replayMax*4; i++ {
		event := signedSessionTestEvent(t, principal, kindStage, nostr.Timestamp(now.Unix()), nil)
		if rejected, reason := final(context.Background(), event); !rejected || !strings.Contains(reason, "stage is full") {
			t.Fatalf("stage-full event %d reject=%v reason=%q", i, rejected, reason)
		}
		policy.mu.Lock()
		mapped, queued := len(policy.replayByID), policy.replay.Len()
		policy.mu.Unlock()
		if mapped != 0 || queued != 0 {
			t.Fatalf("stage-full event %d grew replay state to map=%d queue=%d", i, mapped, queued)
		}
	}

	full = false
	valid := signedSessionTestEvent(t, principal, kindStage, nostr.Timestamp(now.Unix()), nil)
	if rejected, reason := final(context.Background(), valid); rejected {
		t.Fatalf("valid event was artificially blocked after rejected traffic: %s", reason)
	}
}

func TestFinalSessionHookRechecksRevocationBeforeReservations(t *testing.T) {
	now := time.Unix(1_800_000_000, 0)
	principal, _ := nostr.GetPublicKey(nostr.GeneratePrivateKey())
	member, banned := true, false
	policy := newSessionEventPolicy()
	policy.now = func() time.Time { return now }
	policy.getAuthed = func(context.Context) string { return principal }
	policy.setAuthorizers(
		func(groupID, pubkey string) bool { return member && groupID == "club" && pubkey == principal },
		func(pubkey string) bool { return banned && pubkey == principal },
	)
	stage := &stageGate{countFn: func(_ string, _ string) (int, bool) { return 0, false }}
	final := finalSessionReplayAndStageGate(policy, stage)

	for _, test := range []struct {
		name   string
		revoke func()
	}{
		{name: "membership", revoke: func() { member = false }},
		{name: "ban", revoke: func() { banned = true }},
	} {
		t.Run(test.name, func(t *testing.T) {
			member, banned = true, false
			event := signedSessionTestEvent(t, principal, kindStage, nostr.Timestamp(now.Unix()), nil)
			if rejected, reason := policy.reject(context.Background(), event); rejected {
				t.Fatalf("early validation rejected: %s", reason)
			}
			test.revoke()
			if rejected, _ := final(context.Background(), event); !rejected {
				t.Fatal("final hook accepted an event after authorization was revoked")
			}
			policy.mu.Lock()
			seen := len(policy.replayByID)
			queued := policy.replay.Len()
			policy.mu.Unlock()
			stage.mu.Lock()
			pending := len(stage.pending)
			stage.mu.Unlock()
			if seen != 0 || queued != 0 || pending != 0 {
				t.Fatalf("revoked event left replay=%d/%d stage=%d reservations", seen, queued, pending)
			}
		})
	}
}

func TestUnmarkedMainKeyEventKeepsNormalPrincipal(t *testing.T) {
	sk := nostr.GeneratePrivateKey()
	principal, _ := nostr.GetPublicKey(sk)
	event := &nostr.Event{
		Kind: kindStage, PubKey: principal, CreatedAt: nostr.Now(),
		Tags: nostr.Tags{{"h", "club"}, {"d", "club"}},
	}
	policy := newSessionEventPolicy()
	if rejected, reason := policy.reject(context.Background(), event); rejected {
		t.Fatalf("ordinary main-key event entered session policy: %s", reason)
	}
	if got := effectiveEventPubKey(event); got != principal {
		t.Fatalf("effective pubkey = %q, want author %q", got, principal)
	}
}

func TestSessionPrincipalDrivesStageAndPresenceIndexes(t *testing.T) {
	principal, _ := nostr.GetPublicKey(nostr.GeneratePrivateKey())
	event := signedSessionTestEvent(t, principal, kindStage, nostr.Now(), nostr.Tags{{"since", "123"}})
	c := &conductor{
		stageIdx: map[string]map[string]stageEntry{},
		pres:     map[string]map[string]int64{},
	}
	c.idxStage(event)
	if _, ok := c.stageIdx["club"][principal]; !ok {
		t.Fatalf("stage index not keyed by principal: %+v", c.stageIdx)
	}
	if _, ok := c.stageIdx["club"][event.PubKey]; ok {
		t.Fatalf("stage index retained session-key alias %q", event.PubKey)
	}

	presence := signedSessionTestEvent(t, principal, kindPresence, nostr.Now(), nil)
	c.observePresence(context.Background(), presence)
	if _, ok := c.pres["club"][principal]; !ok {
		t.Fatalf("presence index not keyed by principal: %+v", c.pres)
	}
}

func TestStageAliasCleanerKeepsNewestEffectivePrincipalState(t *testing.T) {
	db := &badger.BadgerBackend{Path: t.TempDir()}
	if err := db.Init(); err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	cleaner := &stageAliasCleaner{db: db}
	ctx := context.Background()
	mainSK := nostr.GeneratePrivateKey()
	principal, _ := nostr.GetPublicKey(mainSK)
	base := nostr.Now()

	first := signedSessionTestEvent(t, principal, kindStage, base, nostr.Tags{{"d", "club"}, {"since", "1"}})
	second := signedSessionTestEvent(t, principal, kindStage, base+1, nostr.Tags{{"d", "club"}, {"since", "1"}})
	for _, event := range []*nostr.Event{first, second} {
		if err := db.ReplaceEvent(ctx, event); err != nil {
			t.Fatal(err)
		}
		cleaner.observe(ctx, event)
	}
	assertOnlyStageEvent(t, db, "club", principal, second.ID)

	// A later ordinary main-key heartbeat supersedes every local-key alias too.
	mainEvent := &nostr.Event{
		Kind: kindStage, CreatedAt: base + 2,
		Tags:    nostr.Tags{{"h", "club"}, {"d", "club"}, {"since", "1"}},
		Content: "on",
	}
	if err := mainEvent.Sign(mainSK); err != nil {
		t.Fatal(err)
	}
	if err := db.ReplaceEvent(ctx, mainEvent); err != nil {
		t.Fatal(err)
	}
	cleaner.observe(ctx, mainEvent)
	assertOnlyStageEvent(t, db, "club", principal, mainEvent.ID)
}

func TestStageAliasCleanerDeletesEventThatRacedRevocation(t *testing.T) {
	db := &badger.BadgerBackend{Path: t.TempDir()}
	if err := db.Init(); err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	principal, _ := nostr.GetPublicKey(nostr.GeneratePrivateKey())
	cleaner := &stageAliasCleaner{db: db}
	cleaner.setAuthorizers(
		func(string, string) bool { return false },
		func(string) bool { return false },
	)
	event := signedSessionTestEvent(t, principal, kindStage, nostr.Now(), nostr.Tags{{"d", "club"}})
	if err := db.ReplaceEvent(context.Background(), event); err != nil {
		t.Fatal(err)
	}
	cleaner.observe(context.Background(), event)
	if got := countStageEvents(t, db, "club", principal); got != 0 {
		t.Fatalf("revoked in-flight stage event retained %d rows", got)
	}
}

func TestStageRevocationPurgesMainAndSessionAliases(t *testing.T) {
	db := &badger.BadgerBackend{Path: t.TempDir()}
	if err := db.Init(); err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	cleaner := &stageAliasCleaner{db: db}
	ctx := context.Background()
	mainSK := nostr.GeneratePrivateKey()
	principal, _ := nostr.GetPublicKey(mainSK)

	mainEvent := &nostr.Event{
		Kind: kindStage, CreatedAt: nostr.Now(),
		Tags: nostr.Tags{{"h", "club"}, {"d", "club"}}, Content: "on",
	}
	if err := mainEvent.Sign(mainSK); err != nil {
		t.Fatal(err)
	}
	alias := signedSessionTestEvent(t, principal, kindStage, nostr.Now()+1, nostr.Tags{{"d", "other-address"}})
	for _, event := range []*nostr.Event{mainEvent, alias} {
		if err := db.ReplaceEvent(ctx, event); err != nil {
			t.Fatal(err)
		}
	}
	purged, err := cleaner.purgeClubPrincipal(ctx, "club", principal)
	if err != nil {
		t.Fatal(err)
	}
	if purged != 2 {
		t.Fatalf("purged %d stage rows, want main row and session alias", purged)
	}
	if got := countStageEvents(t, db, "club", principal); got != 0 {
		t.Fatalf("revoked principal retained %d stage rows", got)
	}
}

func TestStageAliasPurgePropagatesStoreFailure(t *testing.T) {
	cleaner := &stageAliasCleaner{db: failingStageAliasStore{}}
	if _, err := cleaner.purgeSessionPrincipal(context.Background(), testMember); err == nil {
		t.Fatal("store query failure should be returned")
	}
}

type failingStageAliasStore struct{}

func (failingStageAliasStore) QueryEvents(context.Context, nostr.Filter) (chan *nostr.Event, error) {
	return nil, errors.New("query failed")
}

func (failingStageAliasStore) DeleteEvent(context.Context, *nostr.Event) error {
	return errors.New("delete failed")
}

func TestAdminBanReportsSessionAliasPurgeFailure(t *testing.T) {
	db := &badger.BadgerBackend{Path: t.TempDir()}
	if err := db.Init(); err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	bans := newBanStore(filepath.Join(t.TempDir(), "banned.json"))
	api := &adminAPI{
		db:           db,
		bans:         bans,
		stageAliases: &stageAliasCleaner{db: failingStageAliasStore{}},
	}
	req := httptest.NewRequest(http.MethodPost, "/admin/ban", strings.NewReader(
		`{"pubkey":"`+testMember+`","reason":"spam"}`,
	))
	response := httptest.NewRecorder()
	api.ban(response, req)
	if response.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", response.Code)
	}
	if !bans.isBanned(testMember) {
		t.Fatal("durable ban must remain active when its best-effort purge reports failure")
	}
}

func TestConductorRechecksAndEvictsStageAuthority(t *testing.T) {
	now := time.Now().UnixMilli()
	member := true
	banned := false
	c := &conductor{
		stageIdx: map[string]map[string]stageEntry{
			"club": {"member": {since: 1, lastSeen: now, on: true}},
		},
		kickIdx: map[string]map[string]int64{},
		queueIdx: map[string]map[string]*nostr.Event{
			"club": {"member": {PubKey: "member"}},
		},
		pres:      map[string]map[string]int64{"club": {"member": now}},
		skipIdx:   map[string]*nostr.Event{},
		autoDJIdx: map[string]*nostr.Event{},
		isMember: func(club, pubkey string) bool {
			return member && club == "club" && pubkey == "member"
		},
		isBanned: func(pubkey string) bool { return banned && pubkey == "member" },
	}
	if got := c.activeClubs(context.Background())["club"]; len(got) != 1 {
		t.Fatalf("current member stage = %v, want one DJ", got)
	}
	member = false
	if got := c.activeClubs(context.Background())["club"]; len(got) != 0 {
		t.Fatalf("removed member retained stage authority: %v", got)
	}
	member = true
	banned = true
	if active, onStage := c.countActiveOtherDJs("club", "other"); active != 0 || onStage {
		t.Fatalf("banned member still counted: active=%d onStage=%v", active, onStage)
	}
	banned = false
	c.revokeClubStagePrincipal("club", "member")
	if got := c.activeClubs(context.Background())["club"]; len(got) != 0 {
		t.Fatalf("evicted lease resurrected: %v", got)
	}
	if c.pres["club"] != nil {
		t.Fatalf("evicted principal retained presence: %+v", c.pres)
	}

	c.revokePrincipal("member")
	if c.queueIdx["club"] != nil {
		t.Fatalf("banned principal retained cached queue: %+v", c.queueIdx)
	}
}

func assertOnlyStageEvent(t *testing.T, db *badger.BadgerBackend, club, principal, wantID string) {
	t.Helper()
	ch, err := db.QueryEvents(context.Background(), nostr.Filter{
		Kinds: []int{kindStage}, Tags: nostr.TagMap{"h": []string{club}},
	})
	if err != nil {
		t.Fatal(err)
	}
	var ids []string
	for event := range ch {
		if effectiveEventPubKey(event) == principal {
			ids = append(ids, event.ID)
		}
	}
	if len(ids) != 1 || ids[0] != wantID {
		t.Fatalf("stored effective-principal stage ids = %v, want [%s]", ids, wantID)
	}
}

func countStageEvents(t *testing.T, db *badger.BadgerBackend, club, principal string) int {
	t.Helper()
	ch, err := db.QueryEvents(context.Background(), nostr.Filter{
		Kinds: []int{kindStage}, Tags: nostr.TagMap{"h": []string{club}},
	})
	if err != nil {
		t.Fatal(err)
	}
	count := 0
	for event := range ch {
		if effectiveEventPubKey(event) == principal {
			count++
		}
	}
	return count
}
