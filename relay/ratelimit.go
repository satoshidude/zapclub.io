package main

import (
	"context"
	"sync"
	"time"

	"github.com/nbd-wtf/go-nostr"
)

// Token-Bucket-Rate-Limiter pro pubkey für ausgewählte Event-Kinds. Greift
// ZUSÄTZLICH zum allgemeinen Limiter und ist bewusst streng für Nutzer-Content
// (Chat/Reaktionen), um Flood/Spam zu stoppen, ohne legitime Nutzung zu behindern.
type kindLimiter struct {
	mu           sync.Mutex
	buckets      map[string]*tokenBucket
	burst        float64
	refillPerSec float64
	kinds        map[int]bool
	reason       string
}

type tokenBucket struct {
	tokens float64
	last   time.Time
}

func newKindLimiter(burst, refillPerSec float64, reason string, kinds ...int) *kindLimiter {
	ks := make(map[int]bool, len(kinds))
	for _, k := range kinds {
		ks[k] = true
	}
	return &kindLimiter{
		buckets:      make(map[string]*tokenBucket),
		burst:        burst,
		refillPerSec: refillPerSec,
		kinds:        ks,
		reason:       reason,
	}
}

// reject implementiert die khatru-RejectEvent-Signatur.
func (l *kindLimiter) reject(_ context.Context, evt *nostr.Event) (bool, string) {
	if !l.kinds[evt.Kind] {
		return false, ""
	}
	now := time.Now()
	l.mu.Lock()
	defer l.mu.Unlock()

	b := l.buckets[evt.PubKey]
	if b == nil {
		b = &tokenBucket{tokens: l.burst, last: now}
		l.buckets[evt.PubKey] = b
	}
	// Auffüllen nach vergangener Zeit, gedeckelt auf burst.
	b.tokens += now.Sub(b.last).Seconds() * l.refillPerSec
	if b.tokens > l.burst {
		b.tokens = l.burst
	}
	b.last = now

	if b.tokens < 1 {
		return true, l.reason
	}
	b.tokens--
	return false, ""
}

// sweep entfernt länger inaktive Buckets, damit die per-pubkey-Map nicht unbegrenzt
// wächst (Memory-DoS-Schutz — billig erzeugte Spam-pubkeys hinterließen sonst dauerhaft
// Einträge). Vom 5-Minuten-Ticker in main.go aufgerufen.
func (l *kindLimiter) sweep(idle time.Duration) {
	now := time.Now()
	l.mu.Lock()
	defer l.mu.Unlock()
	for pk, b := range l.buckets {
		if now.Sub(b.last) > idle {
			delete(l.buckets, pk)
		}
	}
}

// IP-basierter Token-Bucket für die teuren yt-dlp-HTTP-Endpunkte (DoS-Schutz).
// Greift ZUSÄTZLICH zum globalen Concurrency-Limit. Der pubkey-Limiter oben gilt nur
// für Nostr-Events, nicht für die HTTP-Routen — daher hier IP-basiert.
type ipLimiter struct {
	mu           sync.Mutex
	buckets      map[string]*tokenBucket
	burst        float64
	refillPerSec float64
}

func newIPLimiter(burst, refillPerSec float64) *ipLimiter {
	return &ipLimiter{
		buckets:      make(map[string]*tokenBucket),
		burst:        burst,
		refillPerSec: refillPerSec,
	}
}

func (l *ipLimiter) allow(ip string) bool {
	now := time.Now()
	l.mu.Lock()
	defer l.mu.Unlock()

	b := l.buckets[ip]
	if b == nil {
		b = &tokenBucket{tokens: l.burst, last: now}
		l.buckets[ip] = b
	}
	b.tokens += now.Sub(b.last).Seconds() * l.refillPerSec
	if b.tokens > l.burst {
		b.tokens = l.burst
	}
	b.last = now

	if b.tokens < 1 {
		return false
	}
	b.tokens--
	return true
}

// sweep entfernt länger inaktive Buckets, damit die Map nicht unbegrenzt wächst.
func (l *ipLimiter) sweep(idle time.Duration) {
	now := time.Now()
	l.mu.Lock()
	defer l.mu.Unlock()
	for ip, b := range l.buckets {
		if now.Sub(b.last) > idle {
			delete(l.buckets, ip)
		}
	}
}
