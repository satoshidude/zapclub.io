package main

import (
	"net/http/httptest"
	"testing"
	"time"
)

// Der Per-IP-Event-Limiter ist nur so gut wie der Token-Bucket darunter. Hier gegen
// ipLimiter direkt: Burst erschöpfen, dann blocken, dann nach Auffüllung wieder durchlassen,
// und sicherstellen, dass verschiedene IPs getrennte Buckets haben (kein Cross-Talk).
func TestIPLimiterBurstAndRefill(t *testing.T) {
	// Burst 3, Auffüllung 1/s. Künstliche Uhr über das last-Feld, kein echtes Sleep.
	l := newIPLimiter(3, 1)
	ip := "203.0.113.7"

	for i := 0; i < 3; i++ {
		if !l.allow(ip) {
			t.Fatalf("token %d should be allowed within burst", i+1)
		}
	}
	if l.allow(ip) {
		t.Fatal("4th call should be blocked: burst exhausted")
	}

	// Bucket um 2 s in die Vergangenheit verschieben → 2 Tokens nachgefüllt.
	l.mu.Lock()
	l.buckets[ip].last = time.Now().Add(-2 * time.Second)
	l.mu.Unlock()

	for i := 0; i < 2; i++ {
		if !l.allow(ip) {
			t.Fatalf("refilled token %d should be allowed", i+1)
		}
	}
	if l.allow(ip) {
		t.Fatal("should block again after refilled tokens are spent")
	}

	// Eine andere IP ist unbeeinflusst (getrennte Buckets).
	if !l.allow("198.51.100.42") {
		t.Fatal("a different IP must have its own fresh bucket")
	}
}

// Inaktive Buckets müssen weggeräumt werden, sonst wächst die Map durch billig erzeugte
// Spam-IPs unbegrenzt (Memory-DoS). sweep entfernt nur Buckets älter als idle.
func TestIPLimiterSweep(t *testing.T) {
	l := newIPLimiter(5, 1)
	l.allow("a")
	l.allow("b")

	l.mu.Lock()
	l.buckets["a"].last = time.Now().Add(-20 * time.Minute) // alt → wird gefegt
	l.mu.Unlock()

	l.sweep(10 * time.Minute)

	l.mu.Lock()
	_, aGone := l.buckets["a"]
	_, bKept := l.buckets["b"]
	l.mu.Unlock()
	if aGone {
		t.Fatal("idle bucket 'a' should have been swept")
	}
	if !bKept {
		t.Fatal("recently-active bucket 'b' should be kept")
	}
}

// clientIP darf NUR dem von Caddy gesetzten X-Real-IP trauen, nie einem client-gelieferten
// X-Forwarded-For — sonst mintet ein Angreifer pro Request einen frischen Bucket und umgeht
// das Limit. Fehlt X-Real-IP, fällt es auf RemoteAddr (hinter Caddy loopback → 1 Bucket).
func TestClientIPTrustsOnlyXRealIP(t *testing.T) {
	r := httptest.NewRequest("GET", "/", nil)
	r.RemoteAddr = "127.0.0.1:55555"
	r.Header.Set("X-Real-IP", "203.0.113.9")
	r.Header.Set("X-Forwarded-For", "1.2.3.4") // muss ignoriert werden
	if got := clientIP(r); got != "203.0.113.9" {
		t.Fatalf("clientIP = %q, want X-Real-IP 203.0.113.9", got)
	}

	r2 := httptest.NewRequest("GET", "/", nil)
	r2.RemoteAddr = "127.0.0.1:55556"
	r2.Header.Set("X-Forwarded-For", "1.2.3.4") // spoofbar → darf NICHT genutzt werden
	if got := clientIP(r2); got != "127.0.0.1" {
		t.Fatalf("clientIP = %q, want loopback fallback 127.0.0.1 (XFF ignored)", got)
	}
}
