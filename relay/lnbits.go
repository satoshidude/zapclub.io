package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/nbd-wtf/go-nostr"
)

const premiumSats = 2100

// pendingInvoice tracks a not-yet-paid premium subscription invoice.
type pendingInvoice struct {
	Pubkey    string
	Hash      string
	ExpiresAt time.Time
}

// premiumGate handles /premium/invoice and /premium/status endpoints.
// On confirmed payment it calls premiumStore.grant to issue a kind-30108 event.
type premiumGate struct {
	premium    *premiumStore
	lnbitsURL  string // e.g. "https://nsnip.io"
	invoiceKey string // LNbits InvoiceKey (read-only API key)
	mu         sync.Mutex
	pending    map[string]*pendingInvoice // payment_hash → invoice
}

func newPremiumGate(premium *premiumStore, lnbitsURL, invoiceKey string) *premiumGate {
	g := &premiumGate{
		premium:    premium,
		lnbitsURL:  strings.TrimRight(lnbitsURL, "/"),
		invoiceKey: invoiceKey,
		pending:    make(map[string]*pendingInvoice),
	}
	go g.sweep()
	return g
}

// handle dispatches /premium/* HTTP requests.
func (g *premiumGate) handle(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Access-Control-Allow-Origin", allowOrigin)
	w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type")
	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	switch r.URL.Path {
	case "/premium/invoice":
		g.handleInvoice(w, r)
	case "/premium/status":
		g.handleStatus(w, r)
	default:
		http.NotFound(w, r)
	}
}

// handleInvoice creates a new LNbits invoice for a 1-month premium subscription.
// Requires NIP-98 Authorization header (proves the caller's pubkey).
func (g *premiumGate) handleInvoice(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	pubkey, ok := verifyNIP98Pubkey(r)
	if !ok {
		http.Error(w, "unauthorized: valid NIP-98 Authorization required", http.StatusUnauthorized)
		return
	}

	memo := fmt.Sprintf("zapclub.io premium 1 month – %s…", pubkey[:12])
	bolt11, hash, err := g.createInvoice(premiumSats, memo)
	if err != nil {
		log.Printf("lnbits create invoice for %s: %v", pubkey[:12], err)
		http.Error(w, "failed to create invoice", http.StatusBadGateway)
		return
	}

	g.mu.Lock()
	g.pending[hash] = &pendingInvoice{
		Pubkey:    pubkey,
		Hash:      hash,
		ExpiresAt: time.Now().Add(30 * time.Minute),
	}
	g.mu.Unlock()

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{
		"bolt11":       bolt11,
		"payment_hash": hash,
	})
}

// handleStatus checks whether a pending invoice has been paid.
func (g *premiumGate) handleStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	hash := r.URL.Query().Get("hash")
	if hash == "" {
		http.Error(w, "missing hash", http.StatusBadRequest)
		return
	}

	paid, pubkey, err := g.checkAndGrant(r.Context(), hash)
	if err != nil {
		http.Error(w, "lnbits error", http.StatusBadGateway)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	resp := map[string]any{"paid": paid}
	if paid && pubkey != "" {
		resp["pubkey"] = pubkey
	}
	json.NewEncoder(w).Encode(resp)
}

// checkAndGrant queries LNbits for payment status. If paid and pending, grants premium.
// Returns (paid, pubkey, error). pubkey is empty if the hash is unknown or already processed.
func (g *premiumGate) checkAndGrant(ctx context.Context, hash string) (bool, string, error) {
	g.mu.Lock()
	inv, ok := g.pending[hash]
	g.mu.Unlock()
	if !ok {
		// Unknown hash — check LNbits anyway (webhook scenario), but we can't grant without pubkey
		paid, err := g.isPaid(hash)
		return paid, "", err
	}

	paid, err := g.isPaid(hash)
	if err != nil {
		return false, "", err
	}
	if !paid {
		return false, inv.Pubkey, nil
	}

	// Grant and remove from pending
	g.mu.Lock()
	delete(g.pending, hash)
	g.mu.Unlock()

	g.premium.grant(ctx, inv.Pubkey, 1)
	log.Printf("premium granted to %s…", inv.Pubkey[:12])
	return true, inv.Pubkey, nil
}

// sweep cleans up expired pending invoices every 5 minutes.
func (g *premiumGate) sweep() {
	ticker := time.NewTicker(5 * time.Minute)
	for range ticker.C {
		now := time.Now()
		g.mu.Lock()
		for hash, inv := range g.pending {
			if inv.ExpiresAt.Before(now) {
				delete(g.pending, hash)
			}
		}
		g.mu.Unlock()
	}
}

// ── LNbits API calls ─────────────────────────────────────────────────────────

type lnbitsCreateResp struct {
	PaymentHash    string `json:"payment_hash"`
	PaymentRequest string `json:"payment_request"`
}

func (g *premiumGate) createInvoice(sats int, memo string) (bolt11, hash string, err error) {
	body, _ := json.Marshal(map[string]any{
		"out":    false,
		"amount": sats,
		"memo":   memo,
	})
	req, _ := http.NewRequest(http.MethodPost, g.lnbitsURL+"/api/v1/payments", strings.NewReader(string(body)))
	req.Header.Set("X-Api-Key", g.invoiceKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		b, _ := io.ReadAll(resp.Body)
		return "", "", fmt.Errorf("lnbits %d: %s", resp.StatusCode, b)
	}
	var r lnbitsCreateResp
	if err := json.NewDecoder(resp.Body).Decode(&r); err != nil {
		return "", "", err
	}
	return r.PaymentRequest, r.PaymentHash, nil
}

type lnbitsPaymentResp struct {
	Paid bool `json:"paid"`
}

func (g *premiumGate) isPaid(hash string) (bool, error) {
	req, _ := http.NewRequest(http.MethodGet, g.lnbitsURL+"/api/v1/payments/"+hash, nil)
	req.Header.Set("X-Api-Key", g.invoiceKey)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return false, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return false, fmt.Errorf("lnbits status %d", resp.StatusCode)
	}
	var r lnbitsPaymentResp
	if err := json.NewDecoder(resp.Body).Decode(&r); err != nil {
		return false, err
	}
	return r.Paid, nil
}

// ── NIP-98 helper (caller auth, any pubkey) ───────────────────────────────────

// verifyNIP98Pubkey extracts and validates a NIP-98 kind-27235 Authorization header.
// Unlike verifyAdmin it accepts any pubkey (not just superadmin).
func verifyNIP98Pubkey(r *http.Request) (pubkey string, ok bool) {
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
	if ev.Kind != 27235 {
		return "", false
	}
	if sigOk, err := ev.CheckSignature(); !sigOk || err != nil {
		return "", false
	}
	t := ev.CreatedAt.Time()
	now := time.Now()
	if t.Before(now.Add(-60*time.Second)) || t.After(now.Add(60*time.Second)) {
		return "", false
	}
	if m := ev.Tags.GetFirst([]string{"method"}); m == nil || !strings.EqualFold(m.Value(), r.Method) {
		return "", false
	}
	u := ev.Tags.GetFirst([]string{"u"})
	if u == nil {
		return "", false
	}
	parsed, err := url.Parse(u.Value())
	if err != nil || parsed.Path != r.URL.Path {
		return "", false
	}
	return ev.PubKey, true
}
