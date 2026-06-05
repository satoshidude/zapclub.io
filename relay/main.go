package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/fiatjaf/eventstore/badger"
	"github.com/fiatjaf/khatru/policies"
	"github.com/fiatjaf/relay29"
	"github.com/fiatjaf/relay29/khatru29"
	"github.com/nbd-wtf/go-nostr"
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
	admin := &adminAPI{db: db, bans: bans, state: state}
	relay.Router().HandleFunc("/admin/bans", admin.handle)
	relay.Router().HandleFunc("/admin/ban", admin.handle)
	relay.Router().HandleFunc("/admin/unban", admin.handle)
	relay.Router().HandleFunc("/admin/delete-club", admin.handle)

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
		}
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
