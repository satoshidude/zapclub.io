package main

import (
	"context"
	"net/http"
	"strconv"
	"strings"

	"github.com/fiatjaf/khatru"
	"github.com/nbd-wtf/go-nostr"
)

// ipWhitelist holds IPs from RELAY_IP_WHITELIST (comma-separated) that bypass all rate limits.
var ipWhitelist map[string]bool

func initWhitelist() {
	ipWhitelist = map[string]bool{}
	for _, ip := range strings.Split(env("RELAY_IP_WHITELIST", ""), ",") {
		ip = strings.TrimSpace(ip)
		if ip != "" {
			ipWhitelist[ip] = true
		}
	}
}

// Per-IP-Spamschutz + NIP-13 Join-PoW.
//
// Per-IP schließt die Sybil-Lücke: ein einzelner Host erzeugt N Wegwerf-Keypairs, tritt
// offenen Clubs gratis bei (9021) und postet je UNTER dem Per-Pubkey-Limit — pro-IP stoppt das.
//
// Join-PoW (kind 9021): NIP-13 Proof-of-Work mit konfigurierbarer Schwierigkeit (Standard 15).
// Der Client minet vor dem Senden; ~100–500 ms im Browser. Relay prüft CommittedDifficulty
// (nonce-Tag + tatsächliche Leading-Zero-Bits der ID). Schützt vor Massen-Join-Spam auch hinter
// wechselnden IPs/VPNs. Schwierigkeit 0 = deaktiviert (JOIN_POW_DIFFICULTY=0).

// envFloat liest einen Float aus der Umgebung, sonst der Default.
func envFloat(key string, def float64) float64 {
	if v := env(key, ""); v != "" {
		if f, err := strconv.ParseFloat(v, 64); err == nil {
			return f
		}
	}
	return def
}

// envInt liest einen Int aus der Umgebung, sonst der Default.
func envInt(key string, def int) int {
	if v := env(key, ""); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return def
}

// connIP zieht die ECHTE Client-IP aus der WebSocket-Connection im Context. Hinter Caddy ist
// RemoteAddr stets localhost; clientIP() vertraut nur dem von Caddy gesetzten X-Real-IP
// (nicht client-spoofbar). Ist keine Connection im Context (sollte für Events nie passieren),
// fällt es auf einen gemeinsamen Bucket "" zurück (fail-closed, nicht fail-open).
func connIP(ctx context.Context) string {
	if ws := khatru.GetConnection(ctx); ws != nil && ws.Request != nil {
		return clientIP(ws.Request)
	}
	return ""
}

// setupSpamProtection registriert Per-IP-Event- und Per-IP-Connection-Limiter am Relay und
// gibt sie zurück, damit der 5-Minuten-Sweep ihre Maps beschneiden kann. Jeder Limiter lässt
// sich per env abschalten (Burst <= 0 → nicht registriert).
//
// Defaults sind bewusst GROSSZÜGIG: hinter CGNAT/NAT teilen sich viele legitime Nutzer eine
// Quell-IP, die dürfen wir nicht abwürgen. Die Schwelle muss nur unter einer Single-Host-
// Sybil-Flut liegen (N Keypairs × ~30/min Per-Pubkey-Limit), nicht unter normalem Clubbetrieb.
func setupSpamProtection(relay *khatru.Relay) (eventLim, connLim *ipLimiter) {
	// Per-IP-Event-Limiter: zählt ALLE Events einer Quell-IP (inkl. 9021-Joins — das ist
	// genau der Sybil-Vektor). Default 600 Burst, Auffüllung 10/s (~600/min nachhaltig):
	// trägt ~40 aktive NAT-Nutzer, stoppt aber eine Flut aus dutzenden Wegwerf-Keypairs.
	if burst := envFloat("RELAY_IP_EVENT_BURST", 600); burst > 0 {
		eventLim = newIPLimiter(burst, envFloat("RELAY_IP_EVENT_REFILL", 10))
		relay.RejectEvent = append(relay.RejectEvent,
			func(ctx context.Context, _ *nostr.Event) (bool, string) {
				ip := connIP(ctx)
				if ipWhitelist[ip] {
					return false, ""
				}
				if !eventLim.allow(ip) {
					return true, "rate-limited: too many events from your network"
				}
				return false, ""
			},
		)
	}

	// Per-IP-Connection-Limiter: drosselt schnelles WS-Auf-/Zumachen einer Quell-IP
	// (Resource-/Slowloris-artiger DoS). policies.ConnectionRateLimiter nimmt RemoteAddr =
	// localhost hinter Caddy, daher hier eigene clientIP-Variante. Default 60 Burst, 1/s.
	if burst := envFloat("RELAY_IP_CONN_BURST", 60); burst > 0 {
		connLim = newIPLimiter(burst, envFloat("RELAY_IP_CONN_REFILL", 1))
		relay.RejectConnection = append(relay.RejectConnection,
			func(r *http.Request) bool {
				ip := clientIP(r)
				if ipWhitelist[ip] {
					return false
				}
				return !connLim.allow(ip)
			},
		)
	}
	return eventLim, connLim
}
