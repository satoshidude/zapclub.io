package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/fiatjaf/eventstore/badger"
	"github.com/fiatjaf/relay29"
	"github.com/nbd-wtf/go-nostr"
)

// Superadmin (satoshidude). ONLY this pubkey may call the /admin API. Overridable via
// env so the identity isn't hard-baked, but defaults to the known zapclub superadmin.
var superadmin = env("RELAY_SUPERADMIN", "661419f8f48b1b496e2249aee97a6ad9d5bea907149dc7bf3eb7479f2bce555e")

// allowOrigin is the frontend origin permitted to call the admin API (CORS). The relay
// itself sits behind Caddy; the browser enforces this, the auth check below is the teeth.
var allowOrigin = env("RELAY_ADMIN_ORIGIN", "https://zapclub.io")

const adminMutationTimeout = 30 * time.Second

// Once an authenticated destructive request starts, closing the HTTP connection must not cancel
// its durable mutation halfway through. The deadline remains bounded so a wedged store cannot
// hold a handler forever.
func adminMutationContext(_ context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), adminMutationTimeout)
}

// banStore is a relay-wide ban list, persisted as JSON next to the DB so it survives
// restarts and binary swaps (the working dir is persistent across deploys).
type banStore struct {
	mu     sync.RWMutex
	path   string
	banned map[string]string // pubkey -> reason
}

func newBanStore(path string) *banStore {
	b := &banStore{path: path, banned: map[string]string{}}
	if data, err := os.ReadFile(path); err == nil {
		if err := json.Unmarshal(data, &b.banned); err != nil {
			log.Printf("banlist parse (%s): %v — starting empty", path, err)
			b.banned = map[string]string{}
		}
	}
	log.Printf("ban list loaded: %d entries from %s", len(b.banned), path)
	return b
}

func (b *banStore) isBanned(pk string) bool {
	b.mu.RLock()
	defer b.mu.RUnlock()
	_, ok := b.banned[pk]
	return ok
}

func (b *banStore) ban(pk, reason string) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	next := cloneBans(b.banned)
	next[pk] = reason
	if err := b.save(next); err != nil {
		return err
	}
	b.banned = next
	return nil
}

func (b *banStore) unban(pk string) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	next := cloneBans(b.banned)
	delete(next, pk)
	if err := b.save(next); err != nil {
		return err
	}
	b.banned = next
	return nil
}

func (b *banStore) list() map[string]string {
	b.mu.RLock()
	defer b.mu.RUnlock()
	out := make(map[string]string, len(b.banned))
	for k, v := range b.banned {
		out[k] = v
	}
	return out
}

func cloneBans(source map[string]string) map[string]string {
	cloned := make(map[string]string, len(source))
	for pubkey, reason := range source {
		cloned[pubkey] = reason
	}
	return cloned
}

// save persists a proposed list atomically. Caller must hold the write lock; the in-memory
// state is committed only after this succeeds, so a failed ban/unban never creates split-brain
// revocation state between the running relay and its next restart.
func (b *banStore) save(next map[string]string) error {
	data, err := json.MarshalIndent(next, "", "  ")
	if err != nil {
		return fmt.Errorf("encode banlist: %w", err)
	}
	tmp := b.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return fmt.Errorf("write banlist: %w", err)
	}
	if err := os.Rename(tmp, b.path); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("replace banlist: %w", err)
	}
	return nil
}

// verifyNIP98 checks a kind-27235 Authorization header and returns its signer. Path-only URL
// matching keeps it robust behind the Caddy reverse proxy. Tokens are single-use within the
// freshness window, including for read-only private endpoints.
func verifyNIP98(r *http.Request) (string, bool) {
	auth := r.Header.Get("Authorization")
	if !strings.HasPrefix(auth, "Nostr ") {
		return "", false
	}
	raw, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(auth, "Nostr "))
	if err != nil {
		return "", false
	}
	var ev nostr.Event
	if err := json.Unmarshal(raw, &ev); err != nil {
		return "", false
	}
	if ev.Kind != 27235 || !nostr.IsValidPublicKey(ev.PubKey) {
		return "", false
	}
	if ok, err := ev.CheckSignature(); !ok || err != nil {
		return "", false
	}
	// Freshness: ±60s against replay.
	t := ev.CreatedAt.Time()
	now := time.Now()
	if t.Before(now.Add(-60*time.Second)) || t.After(now.Add(60*time.Second)) {
		return "", false
	}
	// Method must match.
	if m := ev.Tags.GetFirst([]string{"method"}); m == nil || !strings.EqualFold(m.Value(), r.Method) {
		return "", false
	}
	// URL path must match the request (host is proxied, so compare path only).
	u := ev.Tags.GetFirst([]string{"u"})
	if u == nil {
		return "", false
	}
	parsed, err := url.Parse(u.Value())
	if err != nil || parsed.Path != r.URL.Path {
		return "", false
	}
	// Replay protection: each NIP-98 token (event id) is single-use within its freshness
	// window. A captured Authorization header can't be replayed to re-run ban/delete.
	if ev.ID != "" && adminNonceSeen(ev.ID) {
		return "", false
	}
	return ev.PubKey, true
}

// verifyAdmin additionally restricts a valid NIP-98 request to the configured superadmin.
func verifyAdmin(r *http.Request) bool {
	pubkey, ok := verifyNIP98(r)
	return ok && pubkey == superadmin
}

// adminNonces tracks used NIP-98 event ids → their expiry, for single-use enforcement.
var adminNonces sync.Map

func adminNonceSeen(id string) bool {
	exp := time.Now().Add(125 * time.Second) // > the ±60s freshness window
	if prev, loaded := adminNonces.LoadOrStore(id, exp); loaded {
		if prev.(time.Time).After(time.Now()) {
			return true // already used and still within the window
		}
		adminNonces.Store(id, exp)
	}
	return false
}

// pruneAdminNonces drops expired nonces (called from the background sweep).
func pruneAdminNonces() {
	now := time.Now()
	adminNonces.Range(func(k, v any) bool {
		if exp, ok := v.(time.Time); ok && exp.Before(now) {
			adminNonces.Delete(k)
		}
		return true
	})
}

// adminAPI exposes superadmin-only relay management over HTTP (NIP-98 authenticated).
type adminAPI struct {
	db           *badger.BadgerBackend
	bans         *banStore
	state        *relay29.State
	listeners    *listenerStats
	stageAliases *stageAliasCleaner
	onBan        func(pubkey string)
	onDeleteClub func(context.Context, string) error
	onClubPurged func(context.Context, string) error
}

func (a *adminAPI) handle(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Access-Control-Allow-Origin", allowOrigin)
	w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type")
	w.Header().Set("Access-Control-Allow-Methods", "POST, GET, OPTIONS")
	w.Header().Set("Vary", "Origin")
	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if !verifyAdmin(r) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	switch r.URL.Path {
	case "/admin/bans":
		a.writeJSON(w, a.bans.list())
	case "/admin/ban":
		a.ban(w, r)
	case "/admin/unban":
		a.unban(w, r)
	case "/admin/delete-club":
		a.deleteClub(w, r)
	case "/admin/listeners":
		snap := a.listeners.snapshot(time.Now().UnixMilli())
		a.writeJSON(w, snap)
	default:
		http.Error(w, "not found", http.StatusNotFound)
	}
}

func (a *adminAPI) writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

func (a *adminAPI) ban(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Pubkey string `json:"pubkey"`
		Reason string `json:"reason"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || !nostr.IsValidPublicKey(body.Pubkey) {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	if body.Pubkey == superadmin {
		http.Error(w, "cannot ban the superadmin", http.StatusForbidden)
		return
	}
	ctx, cancel := adminMutationContext(r.Context())
	defer cancel()
	if err := a.bans.ban(body.Pubkey, body.Reason); err != nil {
		log.Printf("admin: persist ban %s: %v", body.Pubkey, err)
		http.Error(w, "ban persistence failed", http.StatusInternalServerError)
		return
	}
	if a.onBan != nil {
		a.onBan(body.Pubkey)
	}
	purged, err := a.purgeAuthor(ctx, body.Pubkey)
	if err != nil {
		log.Printf("admin: banned %s but purge incomplete after %d events: %v", body.Pubkey, purged, err)
		http.Error(w, "ban active, durable purge incomplete; retry", http.StatusInternalServerError)
		return
	}
	log.Printf("admin: banned %s (%q), purged %d events", body.Pubkey, body.Reason, purged)
	a.writeJSON(w, map[string]any{"ok": true, "purged": purged})
}

func (a *adminAPI) unban(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Pubkey string `json:"pubkey"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || !nostr.IsValidPublicKey(body.Pubkey) {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	if err := a.bans.unban(body.Pubkey); err != nil {
		log.Printf("admin: persist unban %s: %v", body.Pubkey, err)
		http.Error(w, "unban persistence failed", http.StatusInternalServerError)
		return
	}
	log.Printf("admin: unbanned %s", body.Pubkey)
	a.writeJSON(w, map[string]any{"ok": true})
}

func (a *adminAPI) deleteClub(w http.ResponseWriter, r *http.Request) {
	var body struct {
		GroupID string `json:"groupId"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.GroupID == "" {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	ctx, cancel := adminMutationContext(r.Context())
	defer cancel()
	// Evict the live group from relay29's in-memory map FIRST — otherwise the relay keeps
	// regenerating/serving its 39000/39002 metadata and the club reappears after purging.
	a.state.Groups.Delete(body.GroupID)
	// The conductor, social/privacy indexes, stage admissions and analytics all keep their own
	// runtime state. Revoke it before the durable purge so a running tick cannot recreate
	// relay-authored rows after the delete endpoint returns.
	var runtimeErr error
	if a.onDeleteClub != nil {
		runtimeErr = a.onDeleteClub(ctx, body.GroupID)
	}
	// All club content + management events carry an h-tag = group id.
	n, err := a.purgeFilter(ctx, nostr.Filter{Tags: nostr.TagMap{"h": []string{body.GroupID}}})
	// Relay-signed metadata/admins/members are addressable (d = group id).
	metadata, metadataErr := a.purgeFilter(ctx, nostr.Filter{
		Tags:  nostr.TagMap{"d": []string{body.GroupID}},
		Kinds: []int{39000, 39001, 39002, 39003},
	})
	n += metadata
	if err == nil {
		err = metadataErr
	}
	if err == nil && a.onClubPurged != nil {
		err = a.onClubPurged(ctx, body.GroupID)
	}
	if err == nil {
		err = runtimeErr
	}
	if err != nil {
		log.Printf("admin: delete club %s incomplete after %d events: %v", body.GroupID, n, err)
		http.Error(w, "club disabled, durable purge incomplete; retry", http.StatusInternalServerError)
		return
	}
	log.Printf("admin: deleted club %s (evicted from memory), purged %d events", body.GroupID, n)
	a.writeJSON(w, map[string]any{"ok": true, "purged": n})
}

// purgeAuthor deletes every event authored by a pubkey from the store.
func (a *adminAPI) purgeAuthor(ctx context.Context, pk string) (int, error) {
	total, err := a.purgeFilter(ctx, nostr.Filter{Authors: []string{pk}})
	if a.stageAliases != nil {
		aliases, aliasErr := a.stageAliases.purgeSessionPrincipal(ctx, pk)
		total += aliases
		err = errors.Join(err, aliasErr)
	}
	return total, err
}

// purgeFilter deletes every event matching a filter. The badger store caps a single query
// (~250 events), so we LOOP: each pass collects the current matches, deletes them, and
// repeats until a pass deletes nothing — otherwise a ban/club-delete would leave most of a
// prolific author's / busy club's events in the DB. Bounded by a hard pass cap.
func (a *adminAPI) purgeFilter(ctx context.Context, f nostr.Filter) (int, error) {
	total := 0
	for pass := 0; pass < 2000; pass++ {
		ch, err := a.db.QueryEvents(ctx, f)
		if err != nil {
			log.Printf("purge query: %v", err)
			return total, err
		}
		var evs []*nostr.Event
		for ev := range ch { // drain the channel fully before deleting
			evs = append(evs, ev)
		}
		if len(evs) == 0 {
			return total, nil
		}
		deleted := 0
		var deleteErr error
		for _, ev := range evs {
			if err := a.db.DeleteEvent(ctx, ev); err == nil {
				deleted++
			} else {
				log.Printf("purge delete %s: %v", ev.ID, err)
				if deleteErr == nil {
					deleteErr = err
				}
			}
		}
		total += deleted
		if deleteErr != nil {
			return total, deleteErr
		}
		if deleted == 0 {
			return total, fmt.Errorf("purge made no progress")
		}
	}
	return total, fmt.Errorf("purge exceeded pass limit")
}
