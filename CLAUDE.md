# Projekttitel
zapclub.io ist kollaboratives und dezentrales Social-Music-Streaming. Nostr-Login statt Accounts. Ohne Label-Plattform, ohne Tracking, ohne zentrale Identität. Ein Club, der dir gehört.

Domain zapclub.io

UI-Sprache: **Englisch, einsprachig** (Entscheidung 2026-06-10 — Mehrsprachigkeit/
Sprachumschalter bewusst gestrichen, keine i18n-Infrastruktur einbauen).

## 1. Kernidee

Nutzer betreten "Clubs" — virtuelle Räume. In jedem Club gibt es eine Bühne wo DJs auflegen können
(Free: 2 Slots, Premium: 5 Slots). Dazu haben User eine Playlistenverwaltung (Free: 1 Playlist,
Premium: unbegrenzt). Im Round-Robin entsteht unter allen Bühnen-DJs eine gemeinsame Abfolge.

Alle User im Club hören synchron dasselbe. DJs auf der Bühne können Sats in Form von Zaps erhalten.
Zuhörer zappen den aktuell laufenden Track → der DJ verdient, das Voting wird ökonomisch.

## 2. Funktionsumfang (Stand Juni 2026)

| Feature | Beschreibung | Status |
|---|---|---|
| Nostr-Login | NIP-07 (Extension) + NIP-46 (Bunker) | ✅ live |
| Club erstellen / betreten | NIP-29-Gruppe, offene Clubs | ✅ live |
| Club-Verzeichnis | Top-3 + "All clubs"-Toggle, sortiert nach Mitgliedern | ✅ live |
| DJ-Bühne | 2 Slots (Free) / 5 Slots (Premium); relay-erzwungen (kind 30102 gate) | ✅ live |
| Round-Robin-Liste | Conductor interleavt die Queues der Bühnen-DJs pro Track | ✅ live |
| Playlist-Verwaltung | kind 30104; 1 Playlist (Free) / unbegrenzt (Premium); relay-erzwungen | ✅ live |
| Track abspielen | YouTube IFrame API | ✅ live |
| Synchroner Playback | Drift-korrigierte Position über alle Clients | ✅ live |
| Moderation | Skip, DJ werfen, Ban (relay), Mods ernennen | ✅ live |
| Chat | kind 9, ephemeral | ✅ live |
| Avatare | npub-basiert (Robohash / NIP-05 Profilbild) | ✅ live |
| Zap-Voting | NIP-57 Zap auf aktuellen Track → Animation + Score; NWC-Zahlung | ✅ live |
| Entry-fee Clubs | Sats-Gate relay-erzwungen (`entryfee.go`), Premium-Feature | ✅ live |
| Freemium / Premium | 2.100 sats/Monat; relay-seitige Limits; NIP-57-Verifizierung | ✅ live |
| Private / Einladungs-Clubs | closed+private (relay29), Premium-Feature | 🔧 geplant |
| Folgen (Clubs/DJs) | Social-Graph, NIP-51-Listen | Phase 2 |

## 3. Architektur

```
┌─────────────┐     NIP-07/46      ┌──────────────────┐
│   Browser   │◄──────login───────►│  Nostr Signer    │
│  (Client)   │                    │  (Extension/     │
│             │                    │   Bunker)        │
│  - Player   │                    └──────────────────┘
│  - Sync     │
│  - Zap UI   │     WS subscribe   ┌──────────────────┐
│             │◄──────────────────►│  Nostr Relay     │
└─────────────┘   club/now_playing │  (khatru+relay29)│
	   │          chat/zap-receipts└──────────────────┘
	   │
	   │ Zap (LNURL/NIP-57)         ┌──────────────────┐
	   └───────────────────────────►│ Lightning (DJ-   │
									 │ Wallet / LNURL)  │
									 └──────────────────┘
```

**Sync-Strategie:** Pro Club schreibt genau **einer** das `now_playing` — der **Conductor**
(ältester aktiver Bühnen-DJ, selbstheilend; Club-Besitzer auf der Bühne ist immer Master).
Das vermeidet
Last-Write-Wins auf dem replaceable Event bei bis zu 5 DJs. Der Conductor postet bei
Track-Start ein `now_playing` mit `started_at` (Unix-ms) und Track-Ref. Clients berechnen
die lokale Position aus `now() + offsetMs - started_at`, wobei `offsetMs = sent_at - now()`
aus jedem Heartbeat neu kalibriert wird (Drift-Korrektur). Heartbeat alle ~8 s für
Spätankömmlinge. **Achtung Zeiteinheiten:** Nostr `created_at` ist in **Sekunden**,
`Date.now()` in **Millisekunden** — intern konsequent ms halten, beim Ingest `*1000`.

---

## 4. Nostr-Datenmodell (geplant — Greenfield, noch nichts gebaut)

**Clubs = NIP-29 Relay-based Groups** (nicht der kind-30078-Entwurf).
Das Relay (khatru + relay29) verwaltet Mitgliedschaft, Rollen und Moderation.
Alle Club-Events tragen einen `h`-Tag = Gruppen-id.

### Club-Verwaltung (NIP-29)
- `9007` create-group, `9002` edit-metadata (`name`, `about`, `picture`, `open`, `public`)
- `9021` join-request → Relay `9000` add-member · `9022` leave
- Lesen: `39000` Metadaten · `39001` Admins · `39002` Members (mit Rollen)
- Rollen: **Owner/Admin** (`39001`, Creator = Gastgeber, höchste Rechte), `moderator`
  (vom Owner ernannt), `dj` (Bühnen-DJ, max. **5**). Mitglied = in `39002`, ohne Sonderrolle.
- **Subscriptions getrennt** (relay29-Regel): Metadata `{kinds:[39000,39002],"#d":[id]}`,
  Content `{kinds:[9,30100,30102,30103,30106,30107,20100],"#h":[id]}`
- Tag-Regel (playlister-Lektion): `['open']` / `['public']` sind **Single-Element**-Tags —
  `['open','']` wird ignoriert.

### Discovery / Club-Verzeichnis (playlister-bewährt)
**Das Relay ist der Index — keine zentrale DB, kein Announce-Event.** Client listet alle
Clubs via `querySync({kinds:[39000]})` **ohne `#d`-Filter** (relay29 erlaubt globales
Metadata-Listing by design). Neuer Club erscheint automatisch, sobald `9007`+`9002`
akzeptiert sind; auf **≥1 Mitglied** filtern (leere Clubs ausblenden).

### Zugang (MVP: nur offene Clubs)
MVP: alle Clubs `open`+`public` → Beitritt via `9021` ohne Freigabe. Geschlossene/
Einladungs-Clubs und bezahlter Eintritt (Sats-Gate, s. u.) sind Anschlusskonzepte.

### Moderation (MVP)
- **Skip** (aktuellen Track überspringen): erlaubt für **Conductor _und_ Owner/Moderator**
  (auch ohne Bühnen-Slot). Da nur der Conductor `now_playing` schreibt, postet ein
  Owner/Mod ohne Conductor-Rolle eine **Skip-Anfrage** (`kind 30107`, `h`/`d`=club,
  `pos`); der Conductor validiert die Rolle des Absenders (Owner/Mod), prüft `pos` ==
  laufender Track und führt `advance()` aus. Conductor selbst skippt direkt (`pos++`).
  (Owner ohne eigenen Slot wird NICHT Master — er kann nur skippen, nicht die
  Zeit-Autorität an sich ziehen.)
- **DJ von Bühne werfen**: Owner/Mod beendet fremden Bühnen-Slot.
- **Mitglied sperren (Ban)**: **relay-erzwungen** via NIP-29 `9001` remove-user (kick) →
  verliert Membership, kann nicht mehr schreiben. **Achtung offene Clubs:** ein Gekickter
  könnte sofort per `9021` zurück → **dauerhafter Bann braucht zusätzlich eine relay-seitige
  Ban-Liste**, die die pubkey beim Re-Join ablehnt.
- **Moderator ernennen**: Owner setzt Rolle `moderator` via NIP-29 `9000` put-user (→ `39002`).
- **Verwarnen** hat kein eigenes Nostr-Event (kein NIP-29-Kind) → als Chat-/Client-Hinweis
  umsetzen, nicht relay-erzwungen.

### Zeit-Autorität: Conductor + Owner-Override (playlister-bewährt)
Genau **einer** schreibt `now_playing` pro Club, sonst Last-Write-Wins auf dem replaceable
Event. Master-Regel:
- Default: **Conductor = ältester aktiver Bühnen-DJ** (längste Bühnenzeit, deterministisch
  über alle Clients via persistiertem `since`, localStorage). Verlässt er die Bühne,
  übernimmt automatisch der nächste → **selbstheilend**.
- **Ausnahme: Ist der Club-Besitzer als DJ auf der Bühne, ist er immer Master** — unabhängig
  von `since`. (Besitzer = Creator/Admin aus `39001`.)

`since` über Reloads persistieren und aus dem **neuesten** Bühnen-Event lesen — sonst
driften DJ-Sortierung und `pos`-Mapping zwischen Clients auseinander.

**Stream-Stopp / Platzhalter:** Sobald **kein DJ aktiv** ist *oder* die Queue des Masters
**leer** ist, stoppt der Stream und der **Platzhalter (Lobby-Track)** läuft. `live` zählt
nur bei aktiven DJs mit nicht-leerer Queue, sonst `null`.

### Stage / Bühne (parameterized-replaceable, kind 30102)
„Ich bin DJ in diesem Club" — Heartbeat-Event pro DJ (`h`=club). Trägt den Bühnen-Beitritt
(`since`) → Quelle für die Conductor-Reihenfolge. Bis zu 5 DJs (`MAX_DJS`, nach `since`
sortiert, **klebrig** bis ~1 h nach letztem Heartbeat). `since` über Reloads persistieren
und aus dem **neuesten** 30102-Event lesen (nicht erstes einfrieren) — sonst driftet die
DJ-Sortierung zwischen Clients.

### DJ-Queue (parameterized-replaceable, kind 30103)
**Eine Queue pro DJ pro Club** (`d`=club, `h`=club, author=DJ → Adresse
`30103:<dj>:<club>`). Jeder DJ schreibt nur seine eigene → kollisionsfrei. Track-Tag:
`['track','yt:<id>','<title>','<dauer-sek>','off'?]` (Array-Reihenfolge = Ablauf; `off` =
bereits gespielt/inaktiv). Gespeicherte User-Playlisten („Playlistenverwaltung") als
**kind 30104** (Bibliothek), aus der die Club-Queue befüllt wird.

### Round-Robin
Über einen **globalen Integer `pos`** im `now_playing`: `djIndex = pos % n`,
`trackIndex = floor(pos / n)` → `dj0.t0, dj1.t0, …, dj0.t1, …`. `playable(djs)`-Matrix
filtert `off`-Tracks und abwesende DJs; `advance()` sucht den nächsten spielbaren Slot.
Skaliert O(1), die 5 ist nur eine UI-/Slot-Grenze.

### now_playing (parameterized-replaceable, kind 30100)
Genau 1 aktuelle Version pro Club (`d`=club). **Nur der Conductor** überschreibt bei
Track-Wechsel + Heartbeat (~8 s). Tags: `h`, `d`, `track`=`yt:<id>`, `pos` (Round-Robin-
Index), `p`=pubkey des DJs, dessen Track läuft (Zap-Ziel), `started_at` (ms), `sent_at`
(ms, Offset-Kalibrierung), `duration`, `status`. content = `Artist – Title`.
**Track im Heartbeat einfrieren:** denselben Track mit frischem `sent_at` republizieren,
**nicht** aus `pos+Queue` neu ableiten — sonst springt er bei Join/Leave/Umsortieren.
`live`-Track nur werten, wenn aktive DJs **mit nicht-leerer Queue** auf der Bühne sind
(sonst Platzhalter, s. o.).

### skip-request (parameterized-replaceable, kind 30107)
„Bitte den laufenden Track überspringen" — von Owner/Mod **ohne** Conductor-Rolle
(`d`=club, `h`=club, `pos`=laufender Index). Nur der Conductor reagiert darauf und
validiert clientseitig die Rolle des Absenders (das Relay akzeptiert jeden Member-Content,
kennt keine Kind-Allowlist). Replaceable → genau 1 offene Anfrage pro Club.

### chat (kind 9), presence (ephemeral, kind 20100)
Chat per `h`-Tag. Presence ephemeral (nicht persistiert) — vorgesehen.

### zap-vote (später, selektiv aus playlister portieren)
NIP-57 Zap-Receipt mit `h`-Tag auf Club + Referenz aufs now_playing; Empfänger = `p`-Tag
(aktueller DJ). NWC (NIP-47) via `@getalby/sdk` + bitcoin-connect; Score aus verifizierten
9735-Receipts. In playlister bereits gebaut.

### Sats-Eintritts-Gate (✅ live, Premium-Feature)
Bezahlter Club-Eintritt (Preis + Zapper-Key in Club-Config, Beitritt nur gegen gültiges
9735-Receipt, **relay-erzwungen** via `entryfee.go`) ist implementiert und live — nur für
Premium-Club-Besitzer freischaltbar. Membership läuft über NIP-29-Join (`9021`), das Gate ist
eine zusätzliche Relay-Prüfung davor, ohne das Datenmodell zu ändern.

---

## 5. Tech-Stack

- **Frontend:** Svelte 5 (Runes) + Vite + TS. `nostr-tools` für Events/Signing/Subscriptions.
- **Login:** `nostr-login` (NIP-07/NIP-46/Read-Only) + nstart-Onboarding.
- **Zaps (später, selektiv portieren):** `@getalby/sdk` + bitcoin-connect (NWC/NIP-47) für
  NIP-57-Flows.
- **Realtime:** eigenes **NIP-29-Relay** (khatru + relay29, Go, badger),
  `wss://relay.zapclub.io`. Quelle in `relay/` (aus playlister portieren). Details → §7.
- **Audio (MVP):** YouTube IFrame API, Conductor als Zeit-Autorität.
- **Hosting:** statisches Frontend auf `zapclub.io` (Caddy), Relay daneben.
- **Profile (kind 0)** leben im offenen Nostr-Netz (öffentliche Relays), nicht auf dem
  NIP-29-Relay. **Keine** zentrale User-DB, **kein** Tracking.

---

## 6. Freemium-Modell (Stand Juni 2026)

Monetarisierung über ein einfaches Free/Premium-Modell — keine Ads, kein Tracking.
Die Limits sind **relay-seitig** erzwungen (RejectEvent-Hooks in Go), nicht nur im Frontend.

### Tiers

| Limit | Free | Premium (2.100 sats/Monat) |
|---|---|---|
| Clubs erstellen (kind 9007) | 1 | 3 |
| Gespeicherte Playlists (kind 30104) | 1 | unbegrenzt |
| DJs auf der Bühne (kind 30102) | 2 | 5 |
| Clubs joinen / hören / chatten | unbegrenzt | unbegrenzt |
| Zaps senden & empfangen | ✓ | ✓ |
| Private / Einladungs-Clubs | — | ✓ (geplant) |
| Entry-fee Clubs (Sats-Gate) | — | ✓ |
| Featured Listing im Verzeichnis | — | ✓ |

### Relay-Enforcement (Go)

- **`relay/clubcap.go`** — kind 9007 (create-group): zählt bestehende 9007-Events je pubkey.
  Free ≤ 1, Premium ≤ 3. Bestehende Clubs über dem Limit bleiben (Grandfathering).
- **`relay/playlistgate.go`** — kind 30104: zählt distinct d-Tags je pubkey.
  Free ≤ 1, Premium = unbegrenzt. Update desselben d-Tags immer erlaubt.
- **`relay/conductor.go` `stageGate`** — kind 30102 (stage heartbeat): zählt aktive DJs
  (< 1h alt, nicht `off`). Cap basiert auf dem Premium-Status des **Club-Besitzers** (nicht des DJs).
  Free-Club ≤ 2 DJs, Premium-Club ≤ 5 DJs.
- **`relay/premium.go` `premiumStore`** — verifiziert NIP-57-9735-Receipts auf die zapclub-
  Lightning-Adresse, 30-Tage-Fenster. Kein externer API-Aufruf, nur Relay-eigene Events.
- **`relay/entryfee.go`** — Entry-fee-Gate für bezahlte Clubs (Premium-Feature).
- **`SUPERADMIN` env** — pubkey ist von allen Limits ausgenommen (Superadmin-Override).

### Lapse-Verhalten
- Clubs, Playlists und Stage-Slots über dem Free-Limit bleiben erhalten wenn Premium ausläuft.
- Nur **neue Erstellung** über dem Limit wird geblockt.
- Private-Club-Flag und Entry-fee bleiben aktiv (keine Auto-Reopen bei Lapse) — v1 accepted.

### Frontend-Upsell
- `PremiumModal.svelte` — vollständige Feature-Liste, NWC-Zahlung.
- Queue.svelte (DJ Station), UserProfile.svelte (Playlists), ClubList.svelte (Create) — Amber-
  farbene "⚡ … — Premium"-Buttons wenn Limit erreicht.

---

## 6a. Der harte Teil: Audio & Recht

Reihenfolge nach Aufwand/Risiko:

1. **Embed-Sync (MVP):** Alle Clients laden dasselbe YT nur Position wird
   synchronisiert. Kein Hosting, kein Lizenzdeal. Embedding-AGB der Plattform beachten.
2. **P2P (später):** User streamen eigene Dateien via WebRTC mit OBS oder ähnlichem.

---

## 7. Relay & Sicherheit (`wss://relay.zapclub.io`)

Das Relay ist die einzige zentrale Komponente — es muss **schnell, stabil und sicher**
für alle User laufen. Es trägt alle Club-Events (NIP-29), `now_playing`, Chat, Presence.

### Schreibschutz — Fremde schreiben nichts
- **Nur Mitglieder schreiben.** Content-Events (kind 9, 30100, 30102, 30103, 20100) werden
  nur akzeptiert, wenn die `pubkey` Mitglied der Gruppe aus dem `h`-Tag ist (relay29-
  Membership). Nicht-Mitglieder werden abgelehnt.
- **NIP-42 AUTH:** Relay sendet beim Connect eine Challenge (`RequestAuth`). Public-Clubs
  bleiben ohne AUTH lesbar; Schreiben erfordert Mitgliedschaft. Client-AUTH einbauen
  (Robustheit + Vorbereitung private Clubs).
- Relay **lauscht nur auf `127.0.0.1`**, extern per ufw blockiert; Zugriff ausschließlich
  über Caddy-Reverse-Proxy (TLS, WebSocket transparent) + Security-Header (HSTS,
  X-Frame-Options DENY, nosniff, CSP).
- `RELAY_SECRET_KEY` in `relay.env` (mode 600), **nie ins Repo**; persistent halten →
  idempotentes Deploy.

### relay29-Fallstricke (aus playlister, unbedingt übernehmen)
- **relay29 auf `master` pinnen**, nicht Tag v0.5.1 (dort `open`/`closed`-Invertierung →
  Auto-Join offener Clubs kaputt).
- **ReplaceEvent-Handler registrieren:** `relay.ReplaceEvent = append(relay.ReplaceEvent,
  db.ReplaceEvent)` — sonst werden addressable Events (kind 30100) angehäuft statt ersetzt
  (DB-Bloat). Nur per E2E-Test sichtbar, nicht im Code-Review.
- **Metadata-Kinds (39000–39003) nie mit anderen Kinds in einer Subscription mischen** —
  das Relay lehnt ab. Zwei getrennte Subs (s. §4).

### Stabilität & Performance
- **badger klein halten:** Hintergrund-Sweep (5-min-Ticker) räumt Such-Cache + IP-Limiter-
  Buckets → Schutz gegen Memory-DoS und unbegrenztes Wachstum.
- **Deploy-Disziplin:** `go.mod`/`go.sum` eingecheckt; **nie** `go get`/`go mod tidy` auf
  dem Server (bricht `git pull`, stille No-Op-Deploys). Nach Deploy verifizieren, dass das
  neue Feature wirklich im Binary ist (`grep <feature> <binary>`), nicht nur „build ok".
- Server: gehärteter Ubuntu-24.04-VPS (UFW/fail2ban/SSH) — Zugang & Setup-Status nur im
  Memory, nicht in dieser eingecheckten Datei.

---

## 8. Roadmap / Phasen

**✅ MVP + P1 + Monetarisierung — alles live (Stand Juni 2026):**
- Nostr-Login (NIP-07/46/Read-Only), Club erstellen/betreten (offene Clubs)
- Club-Verzeichnis (Top-3 + All), Bühne (Free: 2 / Premium: 5 DJs), Round-Robin, synchroner YT-Playback
- Conductor + Owner-Master, Stop→Platzhalter
- Moderation: Skip, DJ werfen, Ban (relay), Mods ernennen
- Chat (kind 9), Avatare (npub/NIP-05)
- Zaps (NIP-57/NWC) — Track-Voting + Score + Leaderboard, NWC-Zahlung via bitcoin-connect
- Entry-fee Clubs (`entryfee.go`, relay-erzwungen), Premium-Feature
- Freemium-Modell: 2.100 sats/Monat, relay-seitige Limits, NIP-57-Verifizierung
- Playlist-Verwaltung (kind 30104, 1 free / unbegrenzt Premium)

**🔧 Nächstes Feature:**
- **Private / Einladungs-Clubs** (`closed`+`private` relay29-Flags, Premium) — Plan liegt vor.
  Relay-Gate (`privategate.go`), Frontend-Toggle, Request+Approve+Invite-Flow.

**Phase 2 — Social-Graph (Folgen):**
- Club folgen: NIP-51 `kind 10009` (user-groups), getrennt von Membership.
- DJ folgen: **normaler Nostr-Follow** (NIP-02 Kontaktliste, `kind 3`) — global, interoperabel
  mit dem übrigen Nostr-Netz, kein zapclub-eigenes Modell.
- Follow-Listen leben auf **öffentlichen Relays** (User-Level), nicht auf dem Club-Relay.
- Follower-**Zahlen** dezentral teuer/ungenau → MVP nicht zeigen oder nur näherungsweise.

---

## 9. Referenzen (Nostr / Relay) — Stand Juni 2026

**Spezifikationen (NIPs)** — Quelle: `github.com/nostr-protocol/nips` (master), gerenderter
Index: `nips.nostr.com`:
- **NIP-01** Events/Filter/`REQ` — Multi-Filter pro `REQ` **bleibt gültig** (PR #1645 zur
  Single-Filter-Regel wurde abgelehnt; Relays gehen zu komplexitätsbasiertem Rate-Limiting).
- **NIP-29** Relay-based Groups — `nips.nostr.com/29`, Hintergrund PR #566. Maßgeblich für
  Clubs/Rollen/Moderation.
- **NIP-07** Browser-Extension-Signing · **NIP-46** Bunker/Remote-Signing · **NIP-42** AUTH.
- **NIP-02** Kontaktliste (`kind 3`) = DJ folgen · **NIP-51** Listen (`10009` user-groups
  = Club folgen).
- **NIP-57** Zaps (kind 9734/9735) · **NIP-47** NWC (Wallet-Connect).

**Relay (Go, das wir betreiben/portieren):**
- `github.com/fiatjaf/relay29` — NIP-29-Lib + **khatru29**-Wrapper (unsere Basis). Auf
  `master` pinnen (s. §7).
- `github.com/fiatjaf/khatru` — Relay-Framework (+ `eventstore`: badger/LMDB). Doc:
  `khatru.nostr.technology`.
- Vergleichs-Implementierungen zum Studieren: `hoytech/strfry` (+ strfry29),
  `verse-pbc/groups_relay`, `max21dev/groups-relay`, `coracle-social/zooid` (multi-tenant).

**Client-Libs (TS):**
- `github.com/nbd-wtf/nostr-tools` (v2.23.x) — Events/Signing/Pool.
- `github.com/hzrd149/applesauce` — Accounts/Signers (NIP-07/46/Read-Only), wie in playlister.
- `nostr-login` — Login-Widget + nstart-Onboarding (lokaler Signup via `localSignup:true`).
- Zaps: `getAlby/awesome-nwc`, Alby JS SDK (`@getalby/sdk`), bitcoin-connect.

**Referenz-Clients zum Abschauen:** nip29.com „Groups"-Client, noStrudel (applesauce-basiert).

---

## Git
Als satoshidude

## Arbeitsweise
Workflow Orchestration, Task Management und Core Principles: siehe übergeordnete
`claudebase/CLAUDE.md` (wird automatisch mitgeladen) — hier nicht dupliziert.
