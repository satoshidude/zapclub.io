package main

import (
	"context"
	"testing"
	"time"

	"github.com/nbd-wtf/go-nostr"
)

func TestMoodLimiterSharesOneTenSecondBudget(t *testing.T) {
	limiter := newKindLimiter(1, 1.0/10.0, "wait", kindMood)
	banger := &nostr.Event{Kind: kindMood, PubKey: "alice"}
	skip := &nostr.Event{Kind: kindMood, PubKey: "alice"}
	if rejected, _ := limiter.reject(context.Background(), banger); rejected {
		t.Fatal("first reaction must be accepted")
	}
	if rejected, _ := limiter.reject(context.Background(), skip); !rejected {
		t.Fatal("skip immediately after banger must share the cooldown")
	}

	for i := 0; i < 3; i++ {
		limiter.mu.Lock()
		limiter.buckets["alice"].last = time.Now().Add(-10 * time.Second)
		limiter.mu.Unlock()
		if rejected, _ := limiter.reject(context.Background(), skip); rejected {
			t.Fatalf("reaction %d from the same account must be accepted after 10 seconds", i+1)
		}
	}
}

func TestSessionKeysSharePrincipalRateLimit(t *testing.T) {
	principal, _ := nostr.GetPublicKey(nostr.GeneratePrivateKey())
	limiter := newKindLimiter(1, 0, "wait", kindPresence)
	first := signedSessionTestEvent(t, principal, kindPresence, nostr.Now(), nil)
	second := signedSessionTestEvent(t, principal, kindPresence, nostr.Now(), nil)
	if rejected, _ := limiter.reject(context.Background(), first); rejected {
		t.Fatal("first session key must receive the principal's token")
	}
	if rejected, _ := limiter.reject(context.Background(), second); !rejected {
		t.Fatal("rotating the session key bypassed the principal rate limit")
	}
	if len(limiter.buckets) != 1 {
		t.Fatalf("rate limiter created %d buckets, want one principal bucket", len(limiter.buckets))
	}
}
