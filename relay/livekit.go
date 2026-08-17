package main

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/fiatjaf/eventstore/badger"
	"github.com/fiatjaf/relay29"
	"github.com/nbd-wtf/go-nostr"
)

// LiveKit AV spaces token endpoint per NIP-29 LiveKit AV Spaces spec.
//
// Routes (registered in main.go):
//   GET /.well-known/nip29/livekit              → 204 (support-discovery probe)
//   GET /.well-known/nip29/livekit/<group-id>   → { url, token }
//
// Auth: NIP-98 kind-27235 Authorization header (same pattern as /premium/invoice).
// Membership: group must exist and caller must be a member; only staged DJs (+ owner/mod)
// receive canPublish:true. Everyone else is subscribe-only.
//
// JWT: HS256, hand-built — no new Go dependency (respects deploy discipline §7).

type livekitHandler struct {
	db         *badger.BadgerBackend
	state      *relay29.State
	superadmin string
	apiKey     string
	apiSecret  string
	serverURL  string // e.g. wss://live.zapclub.io
}

func newLivekitHandler(db *badger.BadgerBackend, state *relay29.State, superadmin, apiKey, apiSecret, serverURL string) *livekitHandler {
	return &livekitHandler{
		db:         db,
		state:      state,
		superadmin: superadmin,
		apiKey:     apiKey,
		apiSecret:  apiSecret,
		serverURL:  serverURL,
	}
}

func (h *livekitHandler) handle(w http.ResponseWriter, r *http.Request) {
	// Support-discovery probe: GET /.well-known/nip29/livekit (no trailing id)
	path := r.URL.Path
	if path == "/.well-known/nip29/livekit" || path == "/.well-known/nip29/livekit/" {
		w.WriteHeader(http.StatusNoContent)
		return
	}

	// If not configured, decline gracefully so the route still exists.
	if h.apiKey == "" || h.apiSecret == "" || h.serverURL == "" {
		http.Error(w, "livekit not configured", http.StatusServiceUnavailable)
		return
	}

	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Extract group-id from the path suffix.
	groupID := strings.TrimPrefix(path, "/.well-known/nip29/livekit/")
	groupID = strings.TrimSuffix(groupID, "/")
	if groupID == "" {
		http.Error(w, "missing group id", http.StatusBadRequest)
		return
	}

	// NIP-98 authentication.
	pubkey, ok := verifyNIP98Pubkey(r)
	if !ok {
		http.Error(w, "unauthorized: valid NIP-98 Authorization required", http.StatusUnauthorized)
		return
	}

	ctx := r.Context()

	// Membership check.
	if !h.isMember(groupID, pubkey) {
		http.Error(w, "forbidden: not a member of this club", http.StatusForbidden)
		return
	}

	// Determine publish permission.
	canPublish := h.superadmin != "" && pubkey == h.superadmin ||
		h.isMod(groupID, pubkey) ||
		h.isActiveDJ(ctx, groupID, pubkey)

	token, err := h.mintToken(groupID, pubkey, canPublish)
	if err != nil {
		http.Error(w, "token generation failed", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	_ = json.NewEncoder(w).Encode(map[string]string{
		"url":   h.serverURL,
		"token": token,
	})
}

// isMember checks relay29 in-memory group state.
func (h *livekitHandler) isMember(groupID, pk string) (ok bool) {
	defer func() {
		if recover() != nil {
			ok = false
		}
	}()
	group, found := h.state.Groups.Load(groupID)
	if !found || group == nil {
		return false
	}
	_, exists := group.Members[pk]
	return exists
}

// isMod returns true when pk is owner or moderator of the group.
func (h *livekitHandler) isMod(groupID, pk string) (ok bool) {
	defer func() {
		if recover() != nil {
			ok = false
		}
	}()
	group, found := h.state.Groups.Load(groupID)
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

// isActiveDJ returns true when pk has a fresh, non-off stage heartbeat for the group.
func (h *livekitHandler) isActiveDJ(ctx context.Context, groupID, pk string) bool {
	ch, err := h.db.QueryEvents(ctx, nostr.Filter{
		Kinds:   []int{kindStage},
		Tags:    nostr.TagMap{"h": []string{groupID}},
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

// mintToken builds a LiveKit access token (HS256 JWT) without any external library.
// Follows the LiveKit access-token format: header.payload.signature, base64url-encoded.
// Claims reference: https://docs.livekit.io/home/server/generating-tokens/#access-token
func (h *livekitHandler) mintToken(room, pubkey string, canPublish bool) (string, error) {
	now := time.Now().Unix()

	// Random 8-byte suffix to keep sub unique per session.
	suffix := make([]byte, 8)
	if _, err := rand.Read(suffix); err != nil {
		return "", fmt.Errorf("rand: %w", err)
	}
	identity := pubkey + "-" + fmt.Sprintf("%x", suffix)

	header := map[string]string{"alg": "HS256", "typ": "JWT"}
	payload := map[string]interface{}{
		"iss": h.apiKey,
		"sub": identity,
		"iat": now,
		"nbf": now,
		"exp": now + 6*3600, // 6 hours
		"video": map[string]interface{}{
			"room":             room,
			"roomJoin":         true,
			"canPublish":       canPublish,
			"canSubscribe":     true,
			"canPublishData":   true,
			"roomCreate":       false,
			"roomList":         false,
			"roomRecord":       false,
			"roomAdmin":        false,
			"ingressAdmin":     false,
			"hidden":           false,
			"recorder":         false,
		},
	}

	hdrJSON, _ := json.Marshal(header)
	payJSON, _ := json.Marshal(payload)

	hdrEnc := base64.RawURLEncoding.EncodeToString(hdrJSON)
	payEnc := base64.RawURLEncoding.EncodeToString(payJSON)
	sigInput := hdrEnc + "." + payEnc

	mac := hmac.New(sha256.New, []byte(h.apiSecret))
	mac.Write([]byte(sigInput))
	sig := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))

	return sigInput + "." + sig, nil
}
