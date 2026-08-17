package main

// NIP-47 Nostr Wallet Connect client — make_invoice + lookup_invoice only.

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/url"
	"strings"
	"time"

	"github.com/nbd-wtf/go-nostr"
	"github.com/nbd-wtf/go-nostr/nip04"
)

type NWCClient struct {
	walletPK string
	clientSK string
	clientPK string
	relayURL string
}

// parseNWCURL parses a nostr+walletconnect:// URL.
func parseNWCURL(raw string) (*NWCClient, error) {
	u, err := url.Parse(raw)
	if err != nil {
		return nil, err
	}
	walletPK := u.Host
	if walletPK == "" {
		walletPK = strings.TrimPrefix(u.Path, "/")
	}
	q := u.Query()
	relayURL := q.Get("relay")
	clientSK := q.Get("secret")
	if walletPK == "" || relayURL == "" || clientSK == "" {
		return nil, fmt.Errorf("invalid NWC URL: need walletPubkey, relay, and secret")
	}
	clientPK, err := nostr.GetPublicKey(clientSK)
	if err != nil {
		return nil, fmt.Errorf("invalid NWC secret: %v", err)
	}
	return &NWCClient{
		walletPK: walletPK,
		clientSK: clientSK,
		clientPK: clientPK,
		relayURL: relayURL,
	}, nil
}

type nwcEnvelope struct {
	Method string         `json:"method,omitempty"`
	Params map[string]any `json:"params,omitempty"`

	ResultType string          `json:"result_type,omitempty"`
	Error      *nwcErrBody     `json:"error,omitempty"`
	Result     json.RawMessage `json:"result,omitempty"`
}

type nwcErrBody struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// call sends a NIP-47 request and waits for the response. It opens and closes
// its own relay connection so callers don't need to manage state.
func (c *NWCClient) call(ctx context.Context, method string, params map[string]any) (json.RawMessage, error) {
	sharedSecret, err := nip04.ComputeSharedSecret(c.walletPK, c.clientSK)
	if err != nil {
		return nil, fmt.Errorf("NWC shared secret: %v", err)
	}

	reqJSON, _ := json.Marshal(nwcEnvelope{Method: method, Params: params})
	encrypted, err := nip04.Encrypt(string(reqJSON), sharedSecret)
	if err != nil {
		return nil, fmt.Errorf("NWC encrypt: %v", err)
	}

	ev := nostr.Event{
		PubKey:    c.clientPK,
		Kind:      23194,
		CreatedAt: nostr.Timestamp(time.Now().Unix()),
		Tags:      nostr.Tags{{"p", c.walletPK}},
		Content:   encrypted,
	}
	if err := ev.Sign(c.clientSK); err != nil {
		return nil, fmt.Errorf("NWC sign: %v", err)
	}

	relay, err := nostr.RelayConnect(ctx, c.relayURL)
	if err != nil {
		return nil, fmt.Errorf("NWC relay connect: %v", err)
	}
	defer relay.Close()

	// Subscribe for the response BEFORE publishing so we can't miss it.
	sub, err := relay.Subscribe(ctx, []nostr.Filter{{
		Kinds:   []int{23195},
		Authors: []string{c.walletPK},
		Tags:    nostr.TagMap{"e": {ev.ID}},
	}})
	if err != nil {
		return nil, fmt.Errorf("NWC subscribe: %v", err)
	}
	defer sub.Unsub()

	// Fire publish in a goroutine: LNbits' nostrclient relay does not send NIP-01 OK
	// for NWC events, so relay.Publish() would always block until the context deadline.
	// The event is still delivered; we rely on the kind-23195 response subscription below.
	go func() { _ = relay.Publish(context.Background(), ev) }()

	select {
	case resp := <-sub.Events:
		if resp == nil {
			return nil, fmt.Errorf("NWC: nil response event")
		}
		decrypted, err := nip04.Decrypt(resp.Content, sharedSecret)
		if err != nil {
			return nil, fmt.Errorf("NWC decrypt: %v", err)
		}
		var env nwcEnvelope
		if err := json.Unmarshal([]byte(decrypted), &env); err != nil {
			return nil, fmt.Errorf("NWC parse response: %v", err)
		}
		if env.Error != nil {
			return nil, fmt.Errorf("NWC %s: %s", env.Error.Code, env.Error.Message)
		}
		return env.Result, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// MakeInvoice requests a Lightning invoice for sats from the connected wallet.
func (c *NWCClient) MakeInvoice(ctx context.Context, sats int64, description string) (invoice, paymentHash string, err error) {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()

	result, err := c.call(ctx, "make_invoice", map[string]any{
		"amount":      sats * 1000, // NIP-47 amounts are in msats
		"description": description,
	})
	if err != nil {
		return "", "", err
	}
	var r struct {
		Invoice     string `json:"invoice"`
		PaymentHash string `json:"payment_hash"`
	}
	if err := json.Unmarshal(result, &r); err != nil {
		return "", "", fmt.Errorf("NWC parse make_invoice: %v", err)
	}
	return r.Invoice, r.PaymentHash, nil
}

// LookupInvoice returns true if the invoice with the given payment_hash has settled.
func (c *NWCClient) LookupInvoice(ctx context.Context, paymentHash string) (bool, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	result, err := c.call(ctx, "lookup_invoice", map[string]any{
		"payment_hash": paymentHash,
	})
	if err != nil {
		return false, err
	}
	var r struct {
		SettledAt *int64 `json:"settled_at"`
		Preimage  string `json:"preimage"`
	}
	if err := json.Unmarshal(result, &r); err != nil {
		return false, fmt.Errorf("NWC parse lookup_invoice: %v", err)
	}
	// Use settled_at as the sole indicator. Some wallets (e.g. LNbits) return a non-empty
	// preimage placeholder for pending invoices, so checking preimage alone causes false positives.
	settled := r.SettledAt != nil && *r.SettledAt > 0
	log.Printf("[nwc] lookup_invoice hash=%.8s… settled=%v settled_at=%v preimage_len=%d raw=%s",
		paymentHash, settled, r.SettledAt, len(r.Preimage), string(result))
	return settled, nil
}
