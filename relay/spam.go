package main

import (
	"context"
	"net/http"
	"strconv"

	"github.com/fiatjaf/khatru"
	"github.com/nbd-wtf/go-nostr"
)

// Per-IP-Spamschutz. Schließt die Sybil-Lücke, die das Per-Pubkey-Limiting (ratelimit.go)
// offen lässt: Ein einzelner Host erzeugt N Wegwerf-Keypairs, tritt offenen Clubs gratis bei
// (9021) und postet je UNTER dem Per-Pubkey-Limit. Pro-Pubkey greift dann nicht — pro-IP schon,
// weil all diese Keypairs hinter derselben Quell-IP sitzen.
//
// Bewusst NICHT enthalten: NIP-13 Proof-of-Work. PoW würde jeden Client bei jedem Chat/Join
// rechnen lassen (Akku/Latenz auf Mobil) — eine UX-/Produktentscheidung, die nur sinnvoll ist,
// wenn der Client gleichzeitig minen kann. Serverseitig allein aktiviert bräche jeden Write.
// Per-IP kostet keine UX und ist die richtige MVP-Stufe; PoW bleibt offen.

// envFloat liest einen Float aus der Umgebung, sonst der Default.
func envFloat(key string, def float64) float64 {
	if v := env(key, ""); v != "" {
		if f, err := strconv.ParseFloat(v, 64); err == nil {
			return f
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
				if !eventLim.allow(connIP(ctx)) {
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
				return !connLim.allow(clientIP(r))
			},
		)
	}
	return eventLim, connLim
}
