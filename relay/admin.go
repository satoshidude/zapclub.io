package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
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

func (b *banStore) ban(pk, reason string) {
	b.mu.Lock()
	b.banned[pk] = reason
	b.save()
	b.mu.Unlock()
}

func (b *banStore) unban(pk string) {
	b.mu.Lock()
	delete(b.banned, pk)
	b.save()
	b.mu.Unlock()
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

// save persists the list atomically. Caller must hold the write lock.
func (b *banStore) save() {
	data, _ := json.MarshalIndent(b.banned, "", "  ")
	tmp := b.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		log.Printf("banlist save: %v", err)
		return
	}
	if err := os.Rename(tmp, b.path); err != nil {
		log.Printf("banlist rename: %v", err)
	}
}

// verifyAdmin checks the NIP-98 (kind 27235) Authorization header and that the signer is
// the superadmin. Path-only URL match keeps it robust behind the Caddy reverse proxy.
func verifyAdmin(r *http.Request) bool {
	auth := r.Header.Get("Authorization")
	if !strings.HasPrefix(auth, "Nostr ") {
		return false
	}
	raw, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(auth, "Nostr "))
	if err != nil {
		return false
	}
	var ev nostr.Event
	if err := json.Unmarshal(raw, &ev); err != nil {
		return false
	}
	if ev.Kind != 27235 || ev.PubKey != superadmin {
		return false
	}
	if ok, err := ev.CheckSignature(); !ok || err != nil {
		return false
	}
	// Freshness: ±60s against replay.
	t := ev.CreatedAt.Time()
	now := time.Now()
	if t.Before(now.Add(-60*time.Second)) || t.After(now.Add(60*time.Second)) {
		return false
	}
	// Method must match.
	if m := ev.Tags.GetFirst([]string{"method"}); m == nil || !strings.EqualFold(m.Value(), r.Method) {
		return false
	}
	// URL path must match the request (host is proxied, so compare path only).
	u := ev.Tags.GetFirst([]string{"u"})
	if u == nil {
		return false
	}
	parsed, err := url.Parse(u.Value())
	if err != nil || parsed.Path != r.URL.Path {
		return false
	}
	// Replay protection: each NIP-98 token (event id) is single-use within its freshness
	// window. A captured Authorization header can't be replayed to re-run ban/delete.
	if ev.ID != "" && adminNonceSeen(ev.ID) {
		return false
	}
	return true
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
	db    *badger.BadgerBackend
	bans  *banStore
	state *relay29.State
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
	a.bans.ban(body.Pubkey, body.Reason)
	purged := a.purgeAuthor(r.Context(), body.Pubkey)
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
	a.bans.unban(body.Pubkey)
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
	ctx := r.Context()
	// Evict the live group from relay29's in-memory map FIRST — otherwise the relay keeps
	// regenerating/serving its 39000/39002 metadata and the club reappears after purging.
	a.state.Groups.Delete(body.GroupID)
	// All club content + management events carry an h-tag = group id.
	n := a.purgeFilter(ctx, nostr.Filter{Tags: nostr.TagMap{"h": []string{body.GroupID}}})
	// Relay-signed metadata/admins/members are addressable (d = group id).
	n += a.purgeFilter(ctx, nostr.Filter{
		Tags:  nostr.TagMap{"d": []string{body.GroupID}},
		Kinds: []int{39000, 39001, 39002, 39003},
	})
	log.Printf("admin: deleted club %s (evicted from memory), purged %d events", body.GroupID, n)
	a.writeJSON(w, map[string]any{"ok": true, "purged": n})
}

// purgeAuthor deletes every event authored by a pubkey from the store.
func (a *adminAPI) purgeAuthor(ctx context.Context, pk string) int {
	return a.purgeFilter(ctx, nostr.Filter{Authors: []string{pk}})
}

// purgeFilter deletes every event matching a filter. The badger store caps a single query
// (~250 events), so we LOOP: each pass collects the current matches, deletes them, and
// repeats until a pass deletes nothing — otherwise a ban/club-delete would leave most of a
// prolific author's / busy club's events in the DB. Bounded by a hard pass cap.
func (a *adminAPI) purgeFilter(ctx context.Context, f nostr.Filter) int {
	total := 0
	for pass := 0; pass < 2000; pass++ {
		ch, err := a.db.QueryEvents(ctx, f)
		if err != nil {
			log.Printf("purge query: %v", err)
			break
		}
		var evs []*nostr.Event
		for ev := range ch { // drain the channel fully before deleting
			evs = append(evs, ev)
		}
		if len(evs) == 0 {
			break
		}
		deleted := 0
		for _, ev := range evs {
			if err := a.db.DeleteEvent(ctx, ev); err == nil {
				deleted++
			} else {
				log.Printf("purge delete %s: %v", ev.ID, err)
			}
		}
		total += deleted
		if deleted == 0 {
			break // no progress (all deletes failing) — avoid an infinite loop
		}
	}
	return total
}
