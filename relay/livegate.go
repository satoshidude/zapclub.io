package main

import (
	"context"
	"time"

	"github.com/fiatjaf/eventstore/badger"
	"github.com/fiatjaf/relay29"
	"github.com/nbd-wtf/go-nostr"
)

const kindLiveSession = 30109

// liveGate gates kind-30109 (live-session) events: only active staged DJs (or the club
// owner/moderator — for talk-over moderation) may publish a live-session event for a given
// club. This prevents members from forging a takeover that would freeze the conductor or a
// talkover that would duck everyone's audio.
type liveGate struct {
	db         *badger.BadgerBackend
	state      *relay29.State
	superadmin string
}

func newLiveGate(db *badger.BadgerBackend, state *relay29.State, superadmin string) *liveGate {
	return &liveGate{db: db, state: state, superadmin: superadmin}
}

func (g *liveGate) reject(ctx context.Context, evt *nostr.Event) (bool, string) {
	if evt.Kind != kindLiveSession {
		return false, ""
	}
	if g.superadmin != "" && evt.PubKey == g.superadmin {
		return false, ""
	}
	club := tagVal(evt, "h")
	if club == "" {
		return true, "restricted: live-session event missing h tag"
	}

	// Owner/moderator may publish (e.g. talk-over for moderation).
	if g.isMod(club, evt.PubKey) {
		return false, ""
	}

	// Otherwise the author must be an active staged DJ.
	if g.isActiveDJ(ctx, club, evt.PubKey) {
		return false, ""
	}
	return true, "restricted: only staged DJs or club owner/moderators may go live"
}

// isMod returns true when pk is owner or moderator of club (uses relay29 in-memory state,
// same path as isSkipAuthorized in conductor.go).
func (g *liveGate) isMod(club, pk string) (ok bool) {
	defer func() {
		if recover() != nil {
			ok = false
		}
	}()
	group, found := g.state.Groups.Load(club)
	if !found || group == nil {
		return false
	}
	for _, role := range group.Members[pk] {
		if role != nil && (role.Name == "owner" || role.Name == "moderator") {
			return true
		}
	}
	return false
}

// isActiveDJ returns true when pk has a fresh, non-off kind-30102 stage event for club.
func (g *liveGate) isActiveDJ(ctx context.Context, club, pk string) bool {
	ch, err := g.db.QueryEvents(ctx, nostr.Filter{
		Kinds:   []int{kindStage},
		Tags:    nostr.TagMap{"h": []string{club}},
		Authors: []string{pk},
	})
	if err != nil {
		return false
	}
	stale := time.Now().UnixMilli() - condStageStaleMS
	for ev := range ch {
		if ev.Content == "off" {
			continue
		}
		if int64(ev.CreatedAt)*1000 >= stale {
			return true
		}
	}
	return false
}
