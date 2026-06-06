package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/fiatjaf/eventstore/badger"
	"github.com/fiatjaf/khatru/policies"
	"github.com/fiatjaf/relay29"
	"github.com/fiatjaf/relay29/khatru29"
	"github.com/nbd-wtf/go-nostr"
	"github.com/nbd-wtf/go-nostr/nip13"
	"github.com/nbd-wtf/go-nostr/nip29"
)

// Relay-Rollen sind bewusst nur owner + moderator. DJ-Sein ist KEINE NIP-29-Rolle,
// sondern ein Content-Event (kind 30102, Stage) — so kann sich jedes Mitglied selbst
// auf einen freien Bühnen-Slot stellen, ohne dass der Owner eine Rolle vergeben muss.
var (
	ownerRole     = &nip29.Role{Name: "owner", Description: "club owner / host — full control"}
	moderatorRole = &nip29.Role{Name: "moderator", Description: "keeps the club tidy — skip, kick, delete"}
)

func env(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func main() {
	domain := env("RELAY_DOMAIN", "relay.zapclub.io")
	port := env("RELAY_PORT", "3334")
	dbPath := env("RELAY_DB", "./db")

	sk := os.Getenv("RELAY_SECRET_KEY")
	if sk == "" {
		log.Fatal("RELAY_SECRET_KEY not set")
	}

	db := &badger.BadgerBackend{Path: dbPath}
	if err := db.Init(); err != nil {
		log.Fatalf("db init: %v", err)
	}

	relay, state := khatru29.Init(relay29.Options{
		Domain:                  domain,
		DB:                      db,
		SecretKey:               sk,
		DefaultRoles:            []*nip29.Role{ownerRole, moderatorRole},
		GroupCreatorDefaultRole: ownerRole,
	})

	state.AllowAction = func(ctx context.Context, group nip29.Group, role *nip29.Role, action relay29.Action) bool {
		if _, ok := action.(relay29.PutUser); ok {
			// Nur der Owner darf per 9000 Mitglieder/Rollen setzen (z. B. Moderator ernennen).
			// Selbst-Beitritt läuft über 9021 (intern, ohne AllowAction). Verhindert
			// Mod-Eskalation (ein Mod befördert einen Buddy zum Mod).
			return role == ownerRole
		}
		if role == ownerRole {
			return true // owner/host may do everything
		}
		if role == moderatorRole {
			switch action.(type) {
			case relay29.RemoveUser: // Mitglied kicken/bannen
				return true
			case relay29.DeleteEvent: // Track skippen / Chat entfernen
				return true
			}
		}
		return false
	}

	// khatru29.Init registriert nur StoreEvent. Damit addressable Events wie
	// now_playing (kind 30100), stage (30102), queue (30103) ERSETZT statt angehäuft
	// werden, den ReplaceEvent-Handler des Stores ergänzen → genau 1 Zeile pro Adresse.
	relay.ReplaceEvent = append(relay.ReplaceEvent, db.ReplaceEvent)

	// relay29 macht das Verlassen eines Clubs PERMANENT: ein 9022-Leave (wie auch ein
	// Mod-Kick) hinterlegt einen remove-user-Datensatz, und der Join-Reactor lehnt danach
	// JEDEN weiteren Beitritt dieser pubkey ab — sein Check filtert nur nach pubkey, NICHT
	// nach Gruppe, also sperrt eine Entfernung aus EINEM Club das (Wieder-)Beitreten in
	// ALLE. In zapclub ist Entfernen nie ein Dauerbann (das ist die Ban-Liste, früher in der
	// RejectEvent-Kette). Daher VOR dem Join-Reactor die alten remove-user-Sätze löschen.
	relay.OnEventSaved = append(
		[]func(context.Context, *nostr.Event){clearRemovalBarOnJoin(db)},
		relay.OnEventSaved...,
	)

	relay.Info.Name = "zapclub"
	relay.Info.Description = "NIP-29 relay for zapclub.io — collaborative, decentralized music"

	// Relay-wide ban list (superadmin-managed). A banned pubkey can read public clubs but
	// can no longer write ANY event — so it's locked out of joining/DJing/chatting. Checked
	// first (cheapest reject). Their existing events are purged when the ban is issued.
	bans := newBanStore(env("RELAY_BANLIST", "./banned.json"))
	relay.RejectEvent = append(relay.RejectEvent,
		func(_ context.Context, evt *nostr.Event) (bool, string) {
			if bans.isBanned(evt.PubKey) {
				return true, "blocked: banned by the relay administrator"
			}
			return false, ""
		},
	)

	// Per-IP-Spamschutz (schließt die Sybil-Lücke des Per-Pubkey-Limits): Per-IP-Event- +
	// Per-IP-Connection-Limiter. Direkt nach dem Ban-Check (billige Rejects zuerst).
	ipEventLim, ipConnLim := setupSpamProtection(relay)

	// NIP-13 Proof-of-Work on chat (kind 9) — the actual spam vector — taxes mass/IP-distributed
	// posting that per-IP/per-pubkey limits can't fully stop. Cheap to verify (leading zero
	// bits of the id); the client mines slightly above. Per-kind, env-tunable, 0 = off.
	// Join (9021) is NOT gated by default: requiring PoW on join breaks legit joins from any
	// slightly-stale client and adds friction to the core "join an open club" action — while
	// join-floods are already covered by the per-IP limiter. Set RELAY_POW_JOIN>0 to re-enable.
	powJoin := envInt("RELAY_POW_JOIN", 0)
	powChat := envInt("RELAY_POW_CHAT", 10)
	relay.RejectEvent = append(relay.RejectEvent,
		func(_ context.Context, evt *nostr.Event) (bool, string) {
			var min int
			switch evt.Kind {
			case 9021:
				min = powJoin
			case 9:
				min = powChat
			default:
				return false, ""
			}
			if min <= 0 {
				return false, ""
			}
			if nip13.CommittedDifficulty(evt) < min {
				return true, fmt.Sprintf("pow: %d bits of proof-of-work required (NIP-13)", min)
			}
			return false, ""
		},
	)

	// Listener analytics (superadmin dashboard): observe ephemeral presence beats (kind
	// 20100) and keep a rolling 24h per-club record. Persisted so the window survives deploys.
	listeners := newListenerStats(env("RELAY_LISTENERS", "./listeners.json"))
	relay.OnEphemeralEvent = append(relay.OnEphemeralEvent, listeners.observe)

	// Paid-club entry gate: a join (9021) to a club whose owner config (30101) marks it paid
	// must carry a valid NIP-57 zap receipt proving the joiner paid the entry price. Relay-
	// enforced so it can't be bypassed by a hand-crafted join. (Reads the club config from the
	// local store per join — joins are infrequent.)
	entry := newEntryGate(db)
	relay.RejectEvent = append(relay.RejectEvent, entry.reject)

	// Chat (kind 9): streng — Burst 6, Auffüllung 1 alle 3 s (~20/min). Stoppt
	// gescriptete Floods, erlaubt normales Chatten. Kind 9 ist persistent → Spam-Vektor.
	chatLimiter := newKindLimiter(6, 1.0/3.0, "rate-limited: too many chat messages", 9)
	// Presence/Reaktionen (kind 20100, ephemer): Burst 12, Auffüllung 1/s (~60/min).
	reactionLimiter := newKindLimiter(12, 1.0, "rate-limited: too many reactions", 20100)

	relay.RejectEvent = append(relay.RejectEvent,
		// Kind-spezifische, strenge Limiter für Nutzer-Content (vor dem allgemeinen).
		chatLimiter.reject,
		reactionLimiter.reject,
		// Allgemeiner Spam-/Flood-Schutz pro pubkey: Bucket 50 (Burst), 30/min.
		// Deckt strukturelle Events (now_playing-Heartbeat ~8/min, stage, queue, presence).
		// IP-Limit greift hinter Caddy nicht (nur localhost), daher pubkey-basiert.
		// ABER: NIP-29-Verwaltungs-Events (9000–9022) werden NIE rate-limitet — sonst
		// kann ein eventreicher Nutzer (aktiver DJ) den Club nicht mehr VERLASSEN.
		func() func(context.Context, *nostr.Event) (bool, string) {
			limit := policies.EventPubKeyRateLimiter(30, time.Minute, 50)
			return func(ctx context.Context, evt *nostr.Event) (bool, string) {
				if evt.Kind >= 9000 && evt.Kind <= 9022 {
					return false, ""
				}
				return limit(ctx, evt)
			}
		}(),
		policies.PreventLargeTags(128),
		policies.PreventTimestampsInTheFuture(30*time.Second),
		// Content-Größe begrenzen (DB-Bloat-Schutz). 16 KB sind großzügig für
		// Chat/Metadaten; zapclub-Events nutzen Tags, nicht content.
		func(_ context.Context, evt *nostr.Event) (bool, string) {
			if len(evt.Content) > 16*1024 {
				return true, "content too large"
			}
			return false, ""
		},
	)

	relay.Router().HandleFunc("/yt-search", handleSearch)
	relay.Router().HandleFunc("/yt-playlist", handlePlaylist)

	// Superadmin relay management (NIP-98 authenticated, satoshidude only). Registered
	// before the "/" catch-all so the exact paths win.
	admin := &adminAPI{db: db, bans: bans, state: state, listeners: listeners}
	relay.Router().HandleFunc("/admin/bans", admin.handle)
	relay.Router().HandleFunc("/admin/ban", admin.handle)
	relay.Router().HandleFunc("/admin/unban", admin.handle)
	relay.Router().HandleFunc("/admin/delete-club", admin.handle)
	relay.Router().HandleFunc("/admin/listeners", admin.handle)

	relay.Router().HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, "zapclub relay — NIP-29. Use a zapclub/nostr client to connect.")
	})

	// Hintergrund-Sweep: abgelaufene Such-Cache-Einträge + inaktive IP-Limiter-Buckets
	// entfernen, damit die Maps nicht unbegrenzt wachsen (Memory-DoS-Schutz).
	go func() {
		ticker := time.NewTicker(5 * time.Minute)
		defer ticker.Stop()
		for range ticker.C {
			sweepCache()
			ytLimiter.sweep(10 * time.Minute)
			// Per-pubkey content limiters also need sweeping or their maps grow forever.
			chatLimiter.sweep(10 * time.Minute)
			reactionLimiter.sweep(10 * time.Minute)
			if ipEventLim != nil {
				ipEventLim.sweep(10 * time.Minute)
			}
			if ipConnLim != nil {
				ipConnLim.sweep(10 * time.Minute)
			}
			pruneAdminNonces()
			// Advance/trim the listener buckets even when idle, then persist (5-min tick
			// matches the 5-min bucket → at most one bucket lost on an unclean crash).
			listeners.tick(time.Now().UnixMilli(), true)
		}
	}()

	// Persist listener analytics on shutdown (systemctl restart → SIGTERM) so a deploy
	// doesn't drop the last (≤5-min) bucket of the 24h window.
	go func() {
		sig := make(chan os.Signal, 1)
		signal.Notify(sig, syscall.SIGTERM, syscall.SIGINT)
		<-sig
		listeners.save()
		os.Exit(0)
	}()

	// Nur auf localhost lauschen — Zugriff ausschließlich über Caddy (TLS/Routing).
	// Defense-in-Depth: selbst wenn die Firewall mal offen ist, kein direkter Zugriff.
	addr := "127.0.0.1:" + port
	fmt.Printf("zapclub relay (%s) listening on %s\n", domain, addr)
	// ReadHeaderTimeout gegen Slowloris (nacktes ListenAndServe hat keine Timeouts).
	srv := &http.Server{Addr: addr, Handler: relay, ReadHeaderTimeout: 10 * time.Second}
	if err := srv.ListenAndServe(); err != nil {
		log.Fatalf("serve: %v", err)
	}
}
