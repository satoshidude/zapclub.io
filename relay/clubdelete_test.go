package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/fiatjaf/eventstore/badger"
	"github.com/nbd-wtf/go-nostr"
)

func TestConductorDeleteClubEvictsRuntimeAndSQLite(t *testing.T) {
	writer, reader, err := openSQLite(filepath.Join(t.TempDir(), "conductor.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer writer.Close()
	defer reader.Close()
	for _, statement := range []string{
		`INSERT INTO conductor_state(club) VALUES('gone')`,
		`INSERT INTO played(club,dj,video_id,played_at) VALUES('gone','dj','track',1)`,
		`INSERT INTO club_owners(club,owner) VALUES('gone','owner')`,
	} {
		if _, err := writer.Exec(statement); err != nil {
			t.Fatal(err)
		}
	}

	c := newConductor(nil, nil, nil, nostr.GeneratePrivateKey())
	c.sq, c.sqr = writer, reader
	c.clubs["gone"] = &condClub{playing: true}
	c.played["gone"] = map[string]map[string]int64{"dj": {"track": 1}}
	c.bootstrapAt["gone"] = 1
	c.brokenSkipAt["gone"] = 1
	c.moodSkipAt["gone"] = 1
	c.stageIdx["gone"] = map[string]stageEntry{"dj": {on: true}}
	c.kickIdx["gone"] = map[string]int64{"dj": 1}
	c.queueIdx["gone"] = map[string]*nostr.Event{"dj": {}}
	c.skipIdx["gone"] = &nostr.Event{}
	c.autoDJIdx["gone"] = &nostr.Event{}
	c.autoDJCtrlIdx["gone"] = 1
	c.ownerCache["gone"] = "owner"
	c.pres["gone"] = map[string]int64{"dj": 1}
	c.broken["gone"] = map[string]map[string]int64{"track": {"dj": 1}}
	c.brokenVids["gone"] = map[string]int64{"track": 1}
	c.moods["gone"] = map[int]map[string]int{1: {"dj": 1}}
	c.skipCounts["gone"] = map[int]map[string]int{1: {"dj": 1}}
	c.qLogged["gone:dj"] = "fingerprint"
	c.queueWakeup.Store("gone", struct{}{})

	if err := c.deleteClub("gone"); err != nil {
		t.Fatal(err)
	}

	if c.clubs["gone"] != nil || c.played["gone"] != nil || c.stageIdx["gone"] != nil ||
		c.kickIdx["gone"] != nil || c.queueIdx["gone"] != nil || c.skipIdx["gone"] != nil ||
		c.autoDJIdx["gone"] != nil || c.pres["gone"] != nil || c.broken["gone"] != nil ||
		c.brokenVids["gone"] != nil || c.moods["gone"] != nil || c.skipCounts["gone"] != nil {
		t.Fatal("club-scoped conductor state survived deletion")
	}
	if _, ok := c.autoDJCtrlIdx["gone"]; ok {
		t.Fatal("Auto-DJ control index survived deletion")
	}
	if _, ok := c.ownerCache["gone"]; ok {
		t.Fatal("owner cache survived deletion")
	}
	if _, ok := c.qLogged["gone:dj"]; ok {
		t.Fatal("queue log cache survived deletion")
	}
	if _, ok := c.queueWakeup.Load("gone"); ok {
		t.Fatal("queue wakeup survived deletion")
	}
	for _, table := range []string{"conductor_state", "played", "club_owners"} {
		var count int
		if err := reader.QueryRow(`SELECT COUNT(*) FROM ` + table + ` WHERE club='gone'`).Scan(&count); err != nil {
			t.Fatal(err)
		}
		if count != 0 {
			t.Fatalf("SQLite table %s retained %d deleted-club rows", table, count)
		}
	}
}

func TestConductorDeleteClubPropagatesSQLiteFailure(t *testing.T) {
	writer, reader, err := openSQLite(filepath.Join(t.TempDir(), "conductor.db"))
	if err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	defer reader.Close()

	c := newConductor(nil, nil, nil, nostr.GeneratePrivateKey())
	c.sq, c.sqr = writer, reader
	if err := c.deleteClub("gone"); err == nil {
		t.Fatal("closed SQLite writer should make club deletion fail")
	}
}

func TestDeleteClubEvictsSocialStageAndListenerState(t *testing.T) {
	social := &socialGuard{members: map[string]map[string]struct{}{"gone": {"member": {}}}}
	social.deleteClub("gone")
	if social.isMember("gone", "member") {
		t.Fatal("social membership survived deletion")
	}

	stage := &stageGate{pending: map[string]map[string]stageReservation{
		"gone": {"member": {deadline: time.Now().Add(time.Minute), eventID: "event"}},
	}}
	stage.deleteClub("gone")
	if len(stage.pending) != 0 {
		t.Fatal("stage reservation survived deletion")
	}

	listeners := newListenerStats(filepath.Join(t.TempDir(), "listeners.json"))
	listeners.Seen["gone"] = map[string]*span{"session": {First: 1, Last: 2}}
	listeners.Series["gone"] = []listenerSample{{T: 1, N: 1}}
	listeners.CurSets["gone"] = map[string]struct{}{"session": {}}
	listeners.active["gone"] = map[string]int64{"session": 2}
	listeners.published["gone"] = 1
	listeners.lastPublished["gone"] = 2
	listeners.deleteClub("gone")
	if len(listeners.Seen)+len(listeners.Series)+len(listeners.CurSets)+len(listeners.active)+
		len(listeners.published)+len(listeners.lastPublished) != 0 {
		t.Fatal("listener analytics survived deletion")
	}
}

func TestListenerDeleteSerializesConcurrentPersistence(t *testing.T) {
	path := filepath.Join(t.TempDir(), "listeners.json")
	listeners := newListenerStats(path)
	listeners.record("gone", "session", time.Now().UnixMilli())
	var wg sync.WaitGroup
	errs := make(chan error, 33)
	for i := 0; i < 32; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			errs <- listeners.save()
		}()
	}
	wg.Add(1)
	go func() {
		defer wg.Done()
		errs <- listeners.deleteClub("gone")
	}()
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	// Make deletion the final ordered mutation; its snapshot must win over every earlier writer.
	if err := listeners.deleteClub("gone"); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var restored listenerStats
	if err := json.Unmarshal(data, &restored); err != nil {
		t.Fatalf("concurrent listener persistence produced invalid JSON: %v", err)
	}
	if restored.Seen["gone"] != nil || restored.Series["gone"] != nil {
		t.Fatal("an older concurrent listener snapshot restored the deleted club")
	}
}

func TestClubCapDeleteClubReleasesPurgedCreateRows(t *testing.T) {
	db := &badger.BadgerBackend{Path: t.TempDir()}
	if err := db.Init(); err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	sk := nostr.GeneratePrivateKey()
	pubkey, _ := nostr.GetPublicKey(sk)
	event := &nostr.Event{
		Kind: kindCreateGroup, CreatedAt: nostr.Now(),
		Tags: nostr.Tags{{"h", "gone"}},
	}
	if err := event.Sign(sk); err != nil {
		t.Fatal(err)
	}
	if err := db.SaveEvent(context.Background(), event); err != nil {
		t.Fatal(err)
	}
	cap := newClubCap(db, "")
	cap.countIdx[pubkey] = maxClubs
	if err := db.DeleteEvent(context.Background(), event); err != nil {
		t.Fatal(err)
	}
	if err := cap.reload(context.Background()); err != nil {
		t.Fatal(err)
	}
	probe := &nostr.Event{Kind: kindCreateGroup, PubKey: pubkey}
	if rejected, reason := cap.reject(context.Background(), probe); rejected {
		t.Fatalf("purged create row did not release club cap: %s", reason)
	}
}

type blockingClubCapStore struct {
	started chan struct{}
	events  chan *nostr.Event
}

func (s *blockingClubCapStore) QueryEvents(context.Context, nostr.Filter) (chan *nostr.Event, error) {
	close(s.started)
	return s.events, nil
}

func TestClubCapReloadAndConcurrentObservationCountStoredEventOnce(t *testing.T) {
	store := &blockingClubCapStore{
		started: make(chan struct{}),
		events:  make(chan *nostr.Event),
	}
	cap := newClubCap(store, "")
	owner := testOwner
	event := &nostr.Event{ID: "same-create-event", Kind: kindCreateGroup, PubKey: owner}
	reloadDone := make(chan error, 1)
	go func() { reloadDone <- cap.reload(context.Background()) }()
	<-store.started

	observeDone := make(chan struct{})
	go func() {
		cap.observeEvent(context.Background(), event)
		close(observeDone)
	}()
	select {
	case <-observeDone:
		t.Fatal("OnEventSaved observation passed an in-flight reload snapshot")
	case <-time.After(20 * time.Millisecond):
	}
	store.events <- event
	close(store.events)
	if err := <-reloadDone; err != nil {
		t.Fatal(err)
	}
	<-observeDone
	cap.mu.Lock()
	count := cap.countIdx[owner]
	cap.mu.Unlock()
	if count != 1 {
		t.Fatalf("club count = %d, want the stored/observed event exactly once", count)
	}
}
