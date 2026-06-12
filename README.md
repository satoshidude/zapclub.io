# zapclub.io

**Collaborative, decentralized social music streaming.** Step into a *club* — a
virtual room where everyone hears the same track in sync. DJs share a stage and
play back-to-back; listeners **zap real sats** to the DJ that's playing, so
voting becomes economic. **Nostr login** instead of accounts — no label platform,
no tracking, no central identity. A club that belongs to you.

🎧 Live at **[zapclub.io](https://zapclub.io)** · relay at `wss://relay.zapclub.io`

> UI language is English. Music plays from YouTube (embed-sync); identities,
> playlists and zaps live on the open Nostr/Lightning networks.

---

## The idea

Music is more than a playlist. A club is a living room where members listen
together, discover tracks, and shape the sound themselves — the music is chosen
by people, not algorithms. Instead of "likes", appreciation is **real value**:
a listener zaps sats straight to the DJ whose track is playing.

- **No accounts.** You bring your own open identity (Nostr). It's yours, works
  beyond zapclub, and can't be confiscated by the platform. No identity yet? One
  is created in seconds on first visit.
- **No central database, no tracking, no ads.** The relay *is* the index.
  Profiles, playlists and zaps live on public Nostr/Lightning networks.

## Roles

| Role | Can do |
|---|---|
| **Owner** | Creates & edits the club (name, description, image, rules). Appoints moderators; skips tracks; removes DJs from the stage; bans members. Pinned as first in DJ rotation when on stage. |
| **Moderator** | Keeps the club tidy: skip, remove a DJ, delete a message, kick a member. |
| **DJ** | Any member can take a stage slot (Free clubs: up to 2; Premium clubs: up to 5), queue tracks, and earn zaps. |
| **Member** | Joins an open club, listens in sync, chats. |

## Features

- **Nostr login** — NIP-07 extension, NIP-46 bunker (QR), nsec, or a local key.
- **Club directory** — every club is a relay query; the club you're DJing in is
  pinned to the top and pulses. No separate DB.
- **The stage** — up to **2 DJs** on Free clubs, up to **5** on Premium clubs.
  One click to join a free slot; the live DJ pulses green.
- **Round-robin** — each DJ's queue is interleaved into one club set (`dj0.t0,
  dj1.t0, … dj0.t1, …`).
- **Synced playback** — the relay conductor drives position via `now_playing`
  heartbeats; clients drift-correct locally so everyone hears the same moment.
  Stops to a lobby track when no one's on.
- **DJ Station** — search YouTube or import a playlist, reorder by drag-and-drop,
  save/load named playlists (managed on your profile too).
- **Chat & members** per club; **avatars** from the npub (Robohash for people,
  DiceBear "rings" for clubs without a custom image).
- **Zaps (NIP-57)** — a pulsing ⚡ on the live DJ; pay with the Alby extension
  (WebLN), Alby Go on mobile (`alby:` deep link), or any wallet via QR/copy.
  Shows the DJ's total received sats.
- **Moderation** — skip, kick from stage, ban (relay-enforced), appoint mods.

## Freemium model

| | Free | Premium (2,100 sats / month) |
|---|---|---|
| Clubs | 1 | 3 |
| Saved playlists | 1 | unlimited |
| DJs on stage | 2 | 5 |
| Join / listen / chat / zap | ✓ | ✓ |
| Entry-fee clubs | — | ✓ |
| Private / invite-only clubs | — | ✓ |
| Featured in directory | — | ✓ |

Limits are **relay-enforced** (Go `RejectEvent` hooks), not just client-side hints.
Premium is paid via Lightning (NIP-57 zap to the relay's address); auto-renewal
via NWC. Existing clubs and playlists are grandfathered if a subscription lapses.

## How it works

Clubs are **[NIP-29](https://nips.nostr.com/29) relay-based groups**. The relay
(the only central component) enforces membership, roles and moderation, signs the
group metadata events, and **drives playback as the conductor** (see below).
Everything else is plain Nostr — identities, profiles, playlists and zap receipts
live on the open network, not in any zapclub database.

### Event model

All club content carries an `h` = group-id tag. Metadata events are read by `#d`,
content events by `#h` — and the two **must not be mixed in one subscription**
(relay29 rejects that). Times: Nostr `created_at` is in **seconds**, JS time in
**ms** — everything is kept in ms internally and converted at the relay boundary.

| Kind | What |
|---|---|
| `9007` / `9002` | create group / edit metadata (`name`, `about`, `picture`, `open`, `public`) |
| `9021` / `9022` | join / leave · `9000` / `9001` set-role / kick (relay-enforced) |
| `39000–39002` | relay-signed metadata · admins · members (read by `#d`) |
| `30102` | **stage** — "I'm a DJ here" heartbeat; `since` (stage-join time) drives DJ order |
| `30103` | **DJ queue** — one parameterized-replaceable event per DJ/club, the round-robin source |
| `30100` | **now_playing** — *relay-authored*: `track`, `pos`, `started_at`, `sent_at`, `duration`, `p` = the DJ being played (zap target) |
| `1313` | **play-log** — *relay-authored* round-robin progress record (cold-start resume) |
| `30107` | **skip-request** — owner/mod asks the conductor to skip (role-validated relay-side) |
| `20102` | **broken-track** — ephemeral "I can't play this" report (quorum → auto-skip) |
| `30104` / `30101` | playlist library (user-global) · club config (paid-entry gate) |
| `9` / `20100` / `20101` | chat · ephemeral presence · client-side zap broadcast |
| `9734` / `9735` | NIP-57 zap request / receipt (on public relays) |

### The relay is the conductor

A club needs a single, **always-on time authority** so everyone hears the same
moment. Browsers are the opposite of always-on (backgrounded tabs, locked phones,
flaky DJ connections), so zapclub makes the **relay itself** the conductor: it is
the *only* writer of `now_playing` (`30100`) and the play-log (`1313`). The relay
runs a scheduler that, per club with at least one staged DJ and a non-empty queue,
interleaves the DJs' queues round-robin, advances on track end, and republishes
`now_playing`. **Clients are pure consumers** — they read `now_playing`, drift-
correct, and play. They never write it.

This works because relay29 already lets the relay's own key write to any group
without membership (`event.PubKey == s.publicKey`); the conductor signs with
`RELAY_SECRET_KEY` and writes straight to the store, bypassing the reject chain.
Moving the single writer to the always-on relay **deletes an entire class of
problems** the old client-conductor had — leader election, failover, rescue,
sticky-conductor handoff, and the round-robin divergence bugs they caused.

**Persistence (SQLite)** — the conductor keeps a small SQLite file (`conductor.db`)
alongside the BadgerDB event store. BadgerDB is great for Nostr events but its
tag-indexed queries require a full kind-scan; SQLite provides O(1) point-lookups
for four derived/hot-path summaries that don't fit naturally in a key-value store:

| Table | What | Survives restart |
|---|---|---|
| `conductor_state` | current pos, videoID, DJ, started_at per club | ✓ resumes mid-track |
| `played` | offline-DJ played-set (which tracks were already played this session) | ✓ guard intact after crash |
| `club_owners` | club → creator pubkey (immutable, looked up once) | ✓ no re-scan of 9007 events |
| `premium_cache` | per-pubkey premium status (1 h TTL) | ✓ no 30108 scan on cold start |

SQLite is used **alongside** BadgerDB (not replacing it). The event store remains
the source of truth; SQLite holds only derived state that can be rebuilt from events
if the file is lost. The library is pure-Go (`modernc.org/sqlite`, no CGO) so the
relay's static binary stays static.

### Synced playback (drift correction)

The relay republishes `now_playing` on every track change and as a **~15 s
heartbeat** carrying a fresh `sent_at`. Each client calibrates a clock offset
`offset = sent_at − now()` from every heartbeat and computes its local position as
`pos_ms = now() + offset − started_at`. The YouTube player **loads at the
calibrated position** when a track starts, then a **drift check runs every ~5 s**:
if `|player.getCurrentTime() − target| > 3 s`, the client seeks to the correct
position (a 3 s threshold avoids disrupting smooth playback while keeping drift
under 5 s). Late arrivals are in sync within one heartbeat; when no DJ is active or
the queue is empty, the stream stops and a **lobby placeholder** plays.

### Round-robin

A single integer `pos` on `now_playing` indexes the whole club set across `n`
staged DJs: `djIndex = pos % n`, `trackIndex = floor(pos / n)` → `dj0.t0, dj1.t0,
… dj0.t1, …`. A "playable" matrix masks out tracks marked `off` and the currently-playing
track; `advance()` walks to the next playable slot. For **offline DJs** (no recent
`20100` presence beat within 50 s), the relay applies a **played-set guard**: each
track the conductor already played for that DJ is blocked, so their queue drains to
the lobby instead of replaying infinitely. The played-set is recorded in the SQLite
`played` table (survives relay restarts) and also backed by `1313` play-log events.
Online DJs are unaffected — their browser marks tracks `off` directly, which is the
sole truth for present DJs. It's O(1) and the 5-DJ cap is purely a UI limit.
DJ order is deterministic across all clients because it's sorted by the persisted
`since` from each `30102`; a stage slot stays **sticky for ~1 h** after the last
heartbeat so a reload or brief drop doesn't bump a DJ. (When the owner is on stage
they are pinned as the played DJ regardless of `since`.)

### Moderation

- **Skip** — the conductor skips its own current track directly. An owner/mod who
  isn't the conductor posts a `30107` skip-request; the relay **validates the
  sender's role** before acting (a plain member's request is ignored).
- **Broken track** — if a quorum of **2** distinct members report the running
  track unplayable (`20102`), the relay skips it automatically.
- **Ban** — relay-enforced: a `9001` kick removes membership and the admin API
  purges the offender's events; a relay-side ban list rejects re-joins (an open
  club would otherwise let a kicked member walk straight back in via `9021`).
- **Appoint moderator / remove from stage** — owner-gated via NIP-29 roles.

### Audio & zaps

Audio is **embed-sync**: every client loads the *same* YouTube video and only the
*position* is synchronized — no hosting, no re-encoding, no licensing deal (the
embed plays under YouTube's own terms). The IFrame is cross-origin, so the page
can't read its audio samples (hence no real waveform/beat analysis).

**Zaps** are [NIP-57](https://nips.nostr.com/57): the live DJ is the `p` tag on
`now_playing`, their LNURL is resolved from their Nostr profile (falling back to a
house address so the score still credits them), and you pay via the Alby extension
(WebLN), Alby Go on mobile (`alby:` deep link), or any wallet by QR/copy. Because
the wallet's receipt relays don't always publish the `9735` back to us, the client
also broadcasts a `20101` into the club so zaps animate live; the DJ's score is
tallied from verified receipts.

### Security & hardening

- **Members-only writes** — content events are accepted only from a member of the
  group in their `h` tag (relay29 membership); non-members are rejected.
- **Conductor events are relay-only** — `30100`/`1313` are rejected from any
  non-relay author, so clients can't forge or pollute playback state.
- **NIP-42 AUTH** challenge on connect; the relay listens only on `127.0.0.1`
  behind Caddy (TLS, HSTS, `X-Frame-Options: DENY`, `nosniff`).
- **Rate limits** — per-pubkey limits on chat/reactions, per-IP limits on the
  `/yt-search` endpoint (yt-dlp is expensive); NIP-13 proof-of-work on chat.
- **Bounded memory** — 5-minute background sweeps trim the search cache, limiter
  buckets and play-log so badger stays small.

## Tech stack

- **Frontend** — Svelte 5 (Runes) + Vite + TS. `nostr-tools` (events/signing/
  pool), `applesauce-*` (accounts/signers), `qrcode-generator`, `@dicebear/*`.
- **Relay** — Go: `khatru` + `relay29` (pinned to `master`) + `eventstore/badger`
  (LSM-tree event store) + `modernc.org/sqlite` (pure-Go, no CGO — hot-path cache,
  see *Persistence* above).
- **Audio** — YouTube IFrame API; relay is the time authority (`sent_at` heartbeats); clients load at the calibrated position and seek to correct >3 s drift every 5 s.
- **Lightning** — NIP-57 zaps via LNURL; WebLN / Alby Go / any wallet.
- **Hosting** — static frontend + relay behind Caddy on one box.

## Monorepo layout

```
frontend/   Svelte 5 client — player, sync, stage, queue, chat, zaps, profile
relay/      NIP-29 relay (Go) — membership, roles, moderation, content
design/     Branding assets (neon turntable, disco-ball)
deploy/     Caddyfile, systemd unit, placeholder page
```

## Develop

```sh
# Frontend (talks to the live relay by default)
cd frontend && npm install && npm run dev      # http://localhost:5173

# Relay (requires Go + yt-dlp for the /yt-search endpoint)
cd relay && export RELAY_SECRET_KEY=$(openssl rand -hex 32)
go run .                                        # serves on 127.0.0.1:3334
```

## Deploy

Local → prod (no staging). Frontend built and served statically via Caddy on
`zapclub.io`; relay as a hardened systemd service behind Caddy on
`wss://relay.zapclub.io`. The relay is cross-compiled (`GOOS=linux GOARCH=amd64
CGO_ENABLED=0`) and shipped as a static binary — `modernc.org/sqlite` is pure Go
so CGO stays off. Deploy discipline: `go.mod` / `go.sum` committed, **never**
`go get` on the server, verify the feature is in the binary after deploy.
`conductor.db` (SQLite, configured via `SQLITE_PATH`) must be kept across deploys
(it holds conductor state and the premium cache); it is rebuilt from BadgerDB on
first boot if lost, but that causes one cold-start 30108/1313 scan.
