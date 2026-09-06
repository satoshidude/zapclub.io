package main

import (
	"container/list"
	"context"
	"errors"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/fiatjaf/khatru"
	"github.com/nbd-wtf/go-nostr"
)

const (
	sessionEventMarker       = "zapclub-session-v1"
	sessionEventMaxPast      = 60 * time.Second
	sessionEventMaxFuture    = 30 * time.Second
	sessionEventReplayWindow = 2 * time.Minute
	sessionEventReplayMax    = 65_536
)

type sessionReplayRecord struct {
	id     string
	expiry time.Time
}

// sessionEventPolicy is the only exception to relay29's normal author-is-member write rule.
// A page-local key may sign the high-frequency presence and stage-lease events, but the
// authenticated WebSocket identity remains the authority. No durable delegation is created:
// closing the NIP-42-authenticated connection immediately removes the session key's authority.
type sessionEventPolicy struct {
	mu         sync.Mutex
	replay     *list.List               // expiry-ordered sessionReplayRecord values
	replayByID map[string]*list.Element // accepted event id -> exact list element
	replayMax  int
	now        func() time.Time
	getAuthed  func(context.Context) string
	isMember   func(groupID, pubkey string) bool
	isBanned   func(pubkey string) bool
}

func newSessionEventPolicy() *sessionEventPolicy {
	return &sessionEventPolicy{
		replay:     list.New(),
		replayByID: make(map[string]*list.Element),
		replayMax:  sessionEventReplayMax,
		now:        time.Now,
		getAuthed:  khatru.GetAuthed,
	}
}

func (p *sessionEventPolicy) setAuthorizers(
	isMember func(groupID, pubkey string) bool,
	isBanned func(pubkey string) bool,
) {
	p.isMember = isMember
	p.isBanned = isBanned
}

func isSessionEventKind(kind int) bool {
	return kind == kindPresence || kind == kindStage
}

// exactTag returns a value only when a tag occurs exactly once and has exactly the two
// protocol fields. This prevents ambiguous duplicate tags from being interpreted differently
// by the authorization layer and downstream consumers.
func exactTag(tags nostr.Tags, name string) (string, bool) {
	value := ""
	found := false
	for _, tag := range tags {
		if len(tag) == 0 || tag[0] != name {
			continue
		}
		if found || len(tag) != 2 || tag[1] == "" {
			return "", false
		}
		value = tag[1]
		found = true
	}
	return value, found
}

// isSessionEventCandidate intentionally checks only the marker. Marked events must never fall
// through to relay29's generic write rule, even if their kind or remaining tags are malformed.
func isSessionEventCandidate(event *nostr.Event) bool {
	if event == nil {
		return false
	}
	for _, tag := range event.Tags {
		if len(tag) >= 2 && tag[0] == "client" && tag[1] == sessionEventMarker {
			return true
		}
	}
	return false
}

// sessionEventPrincipal parses the strict, downstream-safe alias shape. It does not authorize
// an event; only sessionEventPolicy.reject may do that. Callers use it after the reject chain to
// key state and rate limits by the authenticated main identity rather than the throwaway key.
func sessionEventPrincipal(event *nostr.Event) (string, bool) {
	if event == nil || !isSessionEventKind(event.Kind) {
		return "", false
	}
	marker, markerOK := exactTag(event.Tags, "client")
	principal, principalOK := exactTag(event.Tags, "p")
	if !markerOK || marker != sessionEventMarker || !principalOK || !nostr.IsValidPublicKey(principal) {
		return "", false
	}
	return principal, true
}

func effectiveEventPubKey(event *nostr.Event) string {
	if principal, ok := sessionEventPrincipal(event); ok {
		return principal
	}
	if event == nil {
		return ""
	}
	return event.PubKey
}

// reject validates a marked session event without consuming replay state. Khatru has already
// checked the event id and Schnorr signature before this policy runs; the signature therefore
// remains attributable to the local session key while p is used only as the connection-authorized
// effective principal. Replay reservation deliberately runs in rejectReplay, the final policy,
// so an event rejected by a later limiter or stage gate cannot grow the replay cache.
func (p *sessionEventPolicy) reject(ctx context.Context, event *nostr.Event) (bool, string) {
	if !isSessionEventCandidate(event) {
		return false, ""
	}
	if !isSessionEventKind(event.Kind) {
		return true, "restricted: session keys may publish only presence and stage events"
	}
	principal, ok := sessionEventPrincipal(event)
	if !ok {
		return true, "invalid: malformed zapclub session identity tags"
	}
	groupID, groupOK := exactTag(event.Tags, "h")
	if !groupOK {
		return true, "invalid: session event requires exactly one h tag"
	}
	authed := ""
	if p.getAuthed != nil {
		authed = p.getAuthed(ctx)
	}
	if authed == "" {
		return true, "auth-required: session events require a NIP-42-authenticated connection"
	}
	if authed != principal {
		return true, "restricted: authenticated pubkey does not match session principal"
	}
	if p.isBanned == nil || p.isMember == nil {
		return true, "restricted: session authorization is unavailable"
	}
	if p.isBanned(principal) {
		return true, "blocked: banned by the relay administrator"
	}
	if !p.isMember(groupID, principal) {
		return true, "restricted: session events are for current club members only"
	}

	now := p.now()
	createdAt := event.CreatedAt.Time()
	if createdAt.Before(now.Add(-sessionEventMaxPast)) {
		return true, "invalid: session event is too old"
	}
	if createdAt.After(now.Add(sessionEventMaxFuture)) {
		return true, "invalid: session event timestamp is too far in the future"
	}
	if event.ID == "" {
		return true, "invalid: session event has no id"
	}
	return false, ""
}

// rejectReplay must be the final RejectEvent hook. Its mutex makes simultaneous delivery of the
// same signed event single-winner. The expiry-ordered list and id→element map make reservation,
// rollback and expiration O(1); both structures share the same hard bound. New session events
// fail closed rather than evicting a still-replayable id.
func (p *sessionEventPolicy) rejectReplay(_ context.Context, event *nostr.Event) (bool, string) {
	if !isSessionEventCandidate(event) {
		return false, ""
	}
	now := p.now()
	p.mu.Lock()
	p.pruneReplayLocked(now)
	if element, exists := p.replayByID[event.ID]; exists {
		record := element.Value.(sessionReplayRecord)
		if record.expiry.After(now) {
			p.mu.Unlock()
			return true, "duplicate: session event was already accepted"
		}
		p.removeReplayLocked(element)
	}
	if len(p.replayByID) >= p.replayMax {
		p.mu.Unlock()
		return true, "rate-limited: session replay window is full"
	}
	expiry := now.Add(sessionEventReplayWindow)
	p.addReplayLocked(event.ID, expiry)
	p.mu.Unlock()
	return false, ""
}

func (p *sessionEventPolicy) releaseReplay(event *nostr.Event) {
	if !isSessionEventCandidate(event) {
		return
	}
	p.mu.Lock()
	if element, exists := p.replayByID[event.ID]; exists {
		p.removeReplayLocked(element)
	}
	p.mu.Unlock()
}

// finalSessionReplayAndStageGate is the final RejectEvent hook. It reserves a session event id
// before the side-effecting atomic stage admission, then rolls that id back if admission fails.
// Concurrent identical events therefore have one winner, later stage rejects consume no replay
// memory, and no rejection can strand a stage reservation because no hook runs after this one.
func finalSessionReplayAndStageGate(
	sessions *sessionEventPolicy,
	stage *stageGate,
) func(context.Context, *nostr.Event) (bool, string) {
	return func(ctx context.Context, event *nostr.Event) (bool, string) {
		// Authorization is deliberately checked again at the commit boundary. Membership or a
		// relay-wide ban may have changed while earlier rate-limit hooks were running.
		if rejected, reason := sessions.reject(ctx, event); rejected {
			return true, reason
		}
		if rejected, reason := sessions.rejectReplay(ctx, event); rejected {
			return true, reason
		}
		if rejected, reason := stage.reject(ctx, event); rejected {
			sessions.releaseReplay(event)
			return true, reason
		}
		return false, ""
	}
}

func (p *sessionEventPolicy) pruneReplayLocked(now time.Time) {
	for element := p.replay.Front(); element != nil; element = p.replay.Front() {
		record := element.Value.(sessionReplayRecord)
		if record.expiry.After(now) {
			break
		}
		p.removeReplayLocked(element)
	}
}

func (p *sessionEventPolicy) addReplayLocked(id string, expiry time.Time) {
	element := p.replay.PushBack(sessionReplayRecord{id: id, expiry: expiry})
	p.replayByID[id] = element
}

func (p *sessionEventPolicy) removeReplayLocked(element *list.Element) {
	record := element.Value.(sessionReplayRecord)
	delete(p.replayByID, record.id)
	p.replay.Remove(element)
}

// stageAliasCleaner collapses addressable stage rows across rotating local session keys. Badger's
// normal NIP-33 replacement key includes the signing pubkey, so without this second equivalence
// rule each page load would leave another row for the same member and club.
type stageAliasCleaner struct {
	db       stageAliasStore
	mu       sync.Mutex
	isMember func(club, pubkey string) bool
	isBanned func(pubkey string) bool
}

type stageAliasStore interface {
	QueryEvents(context.Context, nostr.Filter) (chan *nostr.Event, error)
	DeleteEvent(context.Context, *nostr.Event) error
}

func (c *stageAliasCleaner) setAuthorizers(
	isMember func(club, pubkey string) bool,
	isBanned func(pubkey string) bool,
) {
	c.isMember = isMember
	c.isBanned = isBanned
}

func (c *stageAliasCleaner) observe(_ context.Context, event *nostr.Event) {
	if c == nil || c.db == nil || event == nil || event.Kind != kindStage {
		return
	}
	club := tagVal(event, "h")
	principal := effectiveEventPubKey(event)
	if club == "" || principal == "" {
		return
	}
	// OnEventSaved runs after persistence. Recheck dynamic authorization so an event that raced
	// a kick/ban cannot survive on disk and become authoritative after a later rejoin/unban.
	cleanupCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if (c.isBanned != nil && c.isBanned(principal)) ||
		(c.isMember != nil && !c.isMember(club, principal)) {
		if err := c.db.DeleteEvent(cleanupCtx, event); err != nil {
			log.Printf("stage alias cleanup [%.8s]: revoke delete %s: %v", club, event.ID, err)
		}
		return
	}

	// Serialize query+delete across concurrent WebSocket writers. The underlying per-author
	// ReplaceEvent has completed before OnEventSaved invokes this callback.
	c.mu.Lock()
	defer c.mu.Unlock()
	ch, err := c.db.QueryEvents(cleanupCtx, nostr.Filter{
		Kinds: []int{kindStage},
		Tags:  nostr.TagMap{"h": []string{club}},
	})
	if err != nil {
		log.Printf("stage alias cleanup [%.8s]: query: %v", club, err)
		return
	}
	var matches []*nostr.Event
	var newest *nostr.Event
	for candidate := range ch {
		if effectiveEventPubKey(candidate) != principal {
			continue
		}
		matches = append(matches, candidate)
		if newest == nil || eventNewer(candidate, newest) {
			newest = candidate
		}
	}
	if newest == nil || len(matches) < 2 {
		return
	}
	for _, old := range matches {
		if old.ID == newest.ID {
			continue
		}
		if err := c.db.DeleteEvent(cleanupCtx, old); err != nil {
			log.Printf("stage alias cleanup [%.8s]: delete %s: %v", club, old.ID, err)
		}
	}
}

// purgeClubPrincipal removes both ordinary main-key rows and strict session-key aliases for one
// revoked membership. Deleting the durable lease prevents a later rejoin/restart from reviving it.
func (c *stageAliasCleaner) purgeClubPrincipal(ctx context.Context, club, principal string) (int, error) {
	if c == nil || c.db == nil || club == "" || principal == "" {
		return 0, nil
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	total, mainErr := c.purgeMatching(ctx, nostr.Filter{
		Kinds:   []int{kindStage},
		Authors: []string{principal},
		Tags:    nostr.TagMap{"h": []string{club}},
	}, func(event *nostr.Event) bool { return effectiveEventPubKey(event) == principal })
	aliases, aliasErr := c.purgeMatching(ctx, nostr.Filter{
		Kinds: []int{kindStage},
		Tags: nostr.TagMap{
			"h":      []string{club},
			"p":      []string{principal},
			"client": []string{sessionEventMarker},
		},
	}, func(event *nostr.Event) bool {
		got, ok := sessionEventPrincipal(event)
		return ok && got == principal
	})
	return total + aliases, errors.Join(mainErr, aliasErr)
}

// purgeSessionPrincipal is the session-alias complement to adminAPI's all-kinds author purge.
// It is intentionally strict so an unrelated event carrying a p tag is never collateral damage.
func (c *stageAliasCleaner) purgeSessionPrincipal(ctx context.Context, principal string) (int, error) {
	if c == nil || c.db == nil || principal == "" {
		return 0, nil
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.purgeMatching(ctx, nostr.Filter{
		Kinds: []int{kindStage},
		Tags: nostr.TagMap{
			"p":      []string{principal},
			"client": []string{sessionEventMarker},
		},
	}, func(event *nostr.Event) bool {
		got, ok := sessionEventPrincipal(event)
		return ok && got == principal
	})
}

// purgeMatching loops because the Badger event store caps individual query batches.
// stageAliasCleaner.mu must be held by the caller.
func (c *stageAliasCleaner) purgeMatching(
	ctx context.Context,
	filter nostr.Filter,
	matches func(*nostr.Event) bool,
) (int, error) {
	total := 0
	for pass := 0; pass < 2000; pass++ {
		ch, err := c.db.QueryEvents(ctx, filter)
		if err != nil {
			return total, fmt.Errorf("stage principal purge query: %w", err)
		}
		var events []*nostr.Event
		for event := range ch {
			if matches(event) {
				events = append(events, event)
			}
		}
		if len(events) == 0 {
			return total, nil
		}
		deleted := 0
		var deleteErr error
		for _, event := range events {
			if err := c.db.DeleteEvent(ctx, event); err != nil {
				deleteErr = errors.Join(deleteErr, fmt.Errorf("delete %s: %w", event.ID, err))
			} else {
				deleted++
			}
		}
		total += deleted
		if deleteErr != nil {
			return total, fmt.Errorf("stage principal purge: %w", deleteErr)
		}
	}
	return total, fmt.Errorf("stage principal purge exceeded pass limit")
}

// eventstore resolves equal CreatedAt values by keeping the lexicographically smaller id.
func eventNewer(candidate, current *nostr.Event) bool {
	return candidate.CreatedAt > current.CreatedAt ||
		(candidate.CreatedAt == current.CreatedAt && candidate.ID < current.ID)
}
