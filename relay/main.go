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

// pruneOldPlays deletes kind-1313 play records older than `cutoffSec`. 1313 is
// non-replaceable (the shared round-robin progress log) and would otherwise grow forever; the
// client only ever reads a short rolling window, so old records are pure DB bloat. Loops
// query+delete (the store caps a single query) until a pass deletes nothing. Keeps badger
// small per CLAUDE.md §7.
func pruneOldPlays(db *badger.BadgerBackend, cutoffSec int64) int {
	ctx := context.Background()
	until := nostr.Timestamp(cutoffSec)
	total := 0
	for pass := 0; pass < 1000; pass++ {
		ch, err := db.QueryEvents(ctx, nostr.Filter{Kinds: []int{1313}, Until: &until})
		if err != nil {
			log.Printf("prune 1313 query: %v", err)
			break
		}
		var evs []*nostr.Event
		for ev := range ch {
			evs = append(evs, ev)
		}
		if len(evs) == 0 {
			break
		}
		for _, ev := range evs {
			if db.DeleteEvent(ctx, ev) == nil {
				total++
			}
		}
	}
	return total
}

// isForeignConductorWrite reports whether an event is a now_playing (30100) / play-log (1313)
// write from anyone but the relay key. The relay is the sole conductor; clients must not author
// these kinds.
func isForeignConductorWrite(kind int, pubkey, relayPub string) bool {
	return (kind == kindNowPlaying || kind == kindPlay) && pubkey != relayPub
}

// purgeForeignNowPlaying deletes any now_playing (30100) NOT authored by the relay key. These
// can only be pre-migration tombstones from the old client-conductor model (clients no longer
// write 30100, and the reject rule now blocks new ones). 30100 is addressable → at most one per
// (author, club), so the total is tiny and a single query returns them all (no store-cap loop
// needed, unlike the high-volume 1313 play-log — which has no foreign authors anyway). The reject
// rule guards both 30100 and 1313 going forward. Idempotent: finds nothing on later boots.
func purgeForeignNowPlaying(db *badger.BadgerBackend, relayPub string) int {
	ctx := context.Background()
	ch, err := db.QueryEvents(ctx, nostr.Filter{Kinds: []int{kindNowPlaying}})
	if err != nil {
		log.Printf("purge foreign now_playing query: %v", err)
		return 0
	}
	var foreign []*nostr.Event
	for ev := range ch {
		if ev.PubKey != relayPub {
			foreign = append(foreign, ev)
		}
	}
	deleted := 0
	for _, ev := range foreign {
		if db.DeleteEvent(ctx, ev) == nil {
			deleted++
		}
	}
	return deleted
}

func main() {
	domain := env("RELAY_DOMAIN", "relay.zapclub.io")
	port := env("RELAY_PORT", "3334")
	dbPath := env("RELAY_DB", "./db")

	sk := os.Getenv("RELAY_SECRET_KEY")
	if sk == "" {
		log.Fatal("RELAY_SECRET_KEY not set")
	}
	// The relay key is the conductor — the SOLE author of now_playing (30100) and the play-log
	// (1313). Derive its pubkey once: used to reject foreign writes of those kinds and to purge
	// any pre-existing foreign copies (see below).
	relayPub, err := nostr.GetPublicKey(sk)
	if err != nil {
		log.Fatalf("derive relay pubkey: %v", err)
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

	// NIP-42 AUTH: set the canonical service URL so the relay-signed challenge carries the
	// correct wss:// URL even when running behind a Caddy reverse proxy. Without this khatru
	// derives the URL from the HTTP request Host (localhost), which would make client-side
	// 22242 signature verification fail and break all private-group reads.
	relay.ServiceURL = env("RELAY_SERVICE_URL", "wss://relay.zapclub.io")

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

	// now_playing (30100) and the play-log (1313) are relay-authored ONLY — the relay is the
	// conductor. The relay's own writes go straight to the store and bypass this chain
	// (conductor.go), so this only blocks CLIENTS: a member writing 30100/1313 used to be stored-
	// but-ignored (clients accept now_playing only from the relay key), leaving stale per-author
	// tombstones in badger. Reject them outright.
	relay.RejectEvent = append(relay.RejectEvent,
		func(_ context.Context, evt *nostr.Event) (bool, string) {
			if isForeignConductorWrite(evt.Kind, evt.PubKey, relayPub) {
				return true, "blocked: now_playing/play-log are relay-authored"
			}
			return false, ""
		},
	)

	// Per-IP-Spamschutz (schließt die Sybil-Lücke des Per-Pubkey-Limits): Per-IP-Event- +
	// Per-IP-Connection-Limiter. Direkt nach dem Ban-Check (billige Rejects zuerst).
	initWhitelist()
	ipEventLim, ipConnLim := setupSpamProtection(relay)

	// NIP-13 Proof-of-Work: per-kind, env-tunable, 0 = off. Join (9021) requires difficulty 15
	// (client mines ~100–500 ms) to stop mass-join Sybil floods from distributed IPs/VPNs.
	// Chat (9) requires difficulty 10. Set env to 0 to disable either.
	powJoin := envInt("RELAY_POW_JOIN", 15)
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

	// Club creation cap: free=1, premium=3. Existing clubs are grandfathered.
	// prem is wired in below once the premiumStore is initialized.
	cap := newClubCap(db, os.Getenv("SUPERADMIN"))

	// Private-club gate: setting ['closed'] or ['private'] on a 9002/9007 requires Premium.
	// Wired here so prem is available; prem is assigned below after newPremiumStore.
	var privGate *privateGate

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

	// The relay IS the conductor: a background scheduler drives now_playing (kind 30100) + the
	// play-log (1313) for every club with active stage DJs, so playback continues round-robin
	// even when no client is in the foreground. Single always-on writer → no client election/
	// failover/rescue. See conductor.go. It also observes presence (20100) to tell present DJs
	// (trust their queue flags) from away ones (played-set guard) — same rule as the client.
	// Premium subscription system: relay-authored kind-30108 status events + LNbits invoicing.
	prem := newPremiumStore(db, relay, sk, relayPub)
	pg := newPremiumGate(prem, env("LNBITS_URL", ""), os.Getenv("LNBITS_INVOICE_KEY"), "./pending_invoices.json")
	entryPrem = prem // gate paid-entry behind premium check
	condPrem = prem  // per-club DJ slot cap (free=2 / premium=5)
	cap.prem = prem  // club creation cap (free=1 / premium=3)
	relay.RejectEvent = append(relay.RejectEvent, cap.reject)

	// Wire private-gate now that prem is available (declared above near cap).
	privGate = newPrivateGate(prem, os.Getenv("SUPERADMIN"))
	relay.RejectEvent = append(relay.RejectEvent, privGate.reject)

	// Stage cap: free clubs may have at most 2 DJs on stage, premium clubs up to 5.
	stageG := &stageGate{db: db, prem: prem, superadmin: os.Getenv("SUPERADMIN")}
	relay.RejectEvent = append(relay.RejectEvent, stageG.reject)

	// Playlist library (kind 30104): free accounts may save 1 playlist, premium unlimited.
	playlistG := newPlaylistGate(db, prem, os.Getenv("SUPERADMIN"))
	relay.RejectEvent = append(relay.RejectEvent, playlistG.reject)

	// Live A/V session gate (kind 30109): only staged DJs + owner/mod may go live.
	liveG := newLiveGate(db, state, os.Getenv("SUPERADMIN"))
	relay.RejectEvent = append(relay.RejectEvent, liveG.reject)

	// Auto DJ config (kind 30105): only the club's premium owner may arm/disarm.
	autoDJG := newAutoDJGate(db, prem, os.Getenv("SUPERADMIN"))
	relay.RejectEvent = append(relay.RejectEvent, autoDJG.reject)

	cond := newConductor(db, relay, state, sk)
	radioMgr := newRadioManager()
	cond.radioMgr = radioMgr
	// SQLite for persistent conductor state (played-set + track state survive restarts)
	// and premium status cache (eliminates per-check BadgerDB scans).
	if sq, err := openSQLite(env("SQLITE_PATH", "./conductor.db")); err != nil {
		log.Printf("sqlite: open failed (%v) — degraded mode (no persistence)", err)
	} else {
		cond.sq = sq
		prem.sq = sq      // share the same DB for premium_cache
		radioMgr.sq = sq  // radio_enabled persistence
	}
	// One-time cleanup of pre-migration foreign now_playing tombstones (idempotent — see fn).
	if n := purgeForeignNowPlaying(db, relayPub); n > 0 {
		log.Printf("startup: purged %d foreign now_playing event(s)", n)
	}
	// Warm all in-memory indexes from BadgerDB (one-time startup scans).
	// After this, the OnEventSaved observers keep everything current — no per-tick DB reads.
	cond.warmIndexes(context.Background())
	cap.warmCount(context.Background())
	playlistG.warmList(context.Background())
	relay.OnEventSaved = append(relay.OnEventSaved, cond.observeEvent, cap.observeEvent, playlistG.observeEvent)
	relay.OnEphemeralEvent = append(relay.OnEphemeralEvent, cond.observePresence, cond.observeBroken, cond.observeMood)
	// Wire callbacks so gates use the conductor's cached lookups instead of raw DB scans.
	stageG.countFn = cond.countActiveOtherDJs
	stageG.isPremOwnerFn = cond.isPremiumOwner
	autoDJG.ownerFn = cond.clubOwner
	go cond.run()

	// Global all-time zap leaderboard, built from the kind-20101 zap broadcasts (leaderboard.go).
	board := newZapBoard(env("RELAY_LEADERBOARD", "./leaderboard.json"))
	relay.OnEphemeralEvent = append(relay.OnEphemeralEvent, board.observe)

	radioH := &radioHandler{mgr: radioMgr, cond: cond}
	relay.Router().Handle("/radio/", radioH) // GET listen + POST start/stop

	relay.Router().HandleFunc("/yt-search", handleSearch)
	relay.Router().HandleFunc("/yt-playlist", handlePlaylist)
	relay.Router().HandleFunc("/leaderboard", board.handleHTTP)
	relay.Router().HandleFunc("/premium/invoice", pg.handle)
	relay.Router().HandleFunc("/premium/status", pg.handle)
	relay.Router().HandleFunc("/premium/check", pg.handle)

	// LiveKit AV spaces (NIP-29 spec): 204 probe + token endpoint.
	lkHandler := newLivekitHandler(
		db, state, os.Getenv("SUPERADMIN"),
		os.Getenv("LIVEKIT_API_KEY"), os.Getenv("LIVEKIT_API_SECRET"),
		env("LIVEKIT_URL", ""),
	)
	relay.Router().HandleFunc("/.well-known/nip29/livekit", lkHandler.handle)
	relay.Router().HandleFunc("/.well-known/nip29/livekit/", lkHandler.handle)

	// Superadmin relay management (NIP-98 authenticated, satoshidude only). Registered
	// before the "/" catch-all so the exact paths win.
	admin := &adminAPI{db: db, bans: bans, state: state, listeners: listeners, prem: prem}
	relay.Router().HandleFunc("/admin/bans", admin.handle)
	relay.Router().HandleFunc("/admin/ban", admin.handle)
	relay.Router().HandleFunc("/admin/unban", admin.handle)
	relay.Router().HandleFunc("/admin/delete-club", admin.handle)
	relay.Router().HandleFunc("/admin/listeners", admin.handle)
	relay.Router().HandleFunc("/admin/grant-premium", admin.handle)

	relay.Router().HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, "zapclub relay — NIP-29. Use a zapclub/nostr client to connect.")
	})

	// NIP-17 DM sender for premium payment confirmation + expiry reminders.
	dm, dmErr := newDMSender(sk)
	if dmErr != nil {
		log.Fatalf("dm sender init: %v", dmErr)
	}
	_ = pg // pg holds premiumGate, used via routes above; dm is used in sweep below

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
			board.save()
			// Drop play-log records older than 24h (client reads only a ≤6h window).
			pruneOldPlays(db, time.Now().Add(-24*time.Hour).Unix())
			// Send renewal reminders for subscriptions expiring within 3 days.
			sendRenewalReminders(context.Background(), prem, dm)
		}
	}()

	// Persist listener analytics on shutdown (systemctl restart → SIGTERM) so a deploy
	// doesn't drop the last (≤5-min) bucket of the 24h window.
	go func() {
		sig := make(chan os.Signal, 1)
		signal.Notify(sig, syscall.SIGTERM, syscall.SIGINT)
		<-sig
		listeners.save()
		board.save()
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
