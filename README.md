# zapclub.io

**Collaborative, decentralized social music streaming.** Step into a *club* — a
virtual room where everyone hears the same track in sync. Up to five DJs share a
stage and play back-to-back; listeners **zap real sats** to the DJ that's
playing, so voting becomes economic. **Nostr login** instead of accounts — no
label platform, no tracking, no central identity. A club that belongs to you.

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
| **Owner** | Creates & edits the club (name, description, image, rules). Appoints moderators; skips tracks; removes DJs from the stage; bans members; is **always the master DJ** when on stage. |
| **Moderator** | Keeps the club tidy: skip, remove a DJ, delete a message, kick a member. |
| **DJ** | Any member can take a free stage slot (up to 5), queue tracks, and earn zaps. |
| **Member** | Joins an open club, listens in sync, chats. |

## Features

- **Nostr login** — NIP-07 extension, NIP-46 bunker (QR), nsec, or a local key.
- **Club directory** — every club is a relay query; the club you're DJing in is
  pinned to the top and pulses. No separate DB.
- **The stage** — up to **5 DJs** back-to-back. Free slots are one click to join;
  the live DJ pulses green.
- **Round-robin** — each DJ's queue is interleaved into one club set (`dj0.t0,
  dj1.t0, … dj0.t1, …`).
- **Synced playback** — a single **conductor** drives a drift-corrected position
  so everyone hears the same moment; stops to a lobby track when no one's on.
- **DJ Station** — search YouTube or import a playlist, reorder by drag-and-drop,
  save/load named playlists (managed on your profile too).
- **Chat & members** per club; **avatars** from the npub (Robohash for people,
  DiceBear "rings" for clubs without a custom image).
- **Zaps (NIP-57)** — a pulsing ⚡ on the live DJ; pay with the Alby extension
  (WebLN), Alby Go on mobile (`lightning:` deep link), or any wallet via QR/copy.
  Shows the DJ's total received sats.
- **Moderation** — skip, kick from stage, ban (relay-enforced), appoint mods.

## How it works

Clubs are **[NIP-29](https://nips.nostr.com/29) relay-based groups**. The relay
(the only central component) enforces membership, roles and moderation, and
signs the group metadata events. Everything else is plain Nostr.

**Event model** (all club content carries an `h` = group-id tag):

| Kind | What |
|---|---|
| `9007` / `9002` | create group / edit metadata (`name`, `about`, `picture`, `open`, `public`) |
| `9021` / `9022` | join / leave · `9000` / `9001` add-member-role / kick |
| `39000–39002` | relay-signed metadata · admins · members (read by `#d`) |
| `30102` | **stage** — "I'm a DJ here" heartbeat (carries `since` → conductor order) |
| `30103` | **DJ queue** — one per DJ/club, round-robin source |
| `30100` | **now_playing** — the conductor's track (`pos`, `started_at`, `sent_at`, `p`=DJ) |
| `30104` | **playlist library** — user-global, on public relays |
| `9` / `20100` | chat · ephemeral presence |
| `9734` / `9735` | NIP-57 zap request / receipt (on public relays) |

**Conductor & sync.** Exactly one client writes `now_playing` — the *conductor*
(oldest active stage DJ, self-healing; the owner-on-stage is always master). It
republishes on track change and as an ~8 s heartbeat with a fresh `sent_at`;
clients calibrate `offset = sent_at − now()` and compute the local position as
`now() + offset − started_at`. (Nostr `created_at` is in **seconds**, JS time in
**ms** — kept in ms internally.)

**Round-robin.** A single integer `pos` in `now_playing`: `djIndex = pos % n`,
`trackIndex = floor(pos / n)`. A playable matrix skips played/absent tracks;
`advance()` finds the next playable slot. Scales O(1); the 5 is just a UI limit.

**Security.** Only members can write content events (relay29 membership), NIP-42
AUTH on the relay, which listens on `127.0.0.1` behind Caddy (TLS + security
headers). Per-pubkey rate limits on chat/reactions; background sweeps keep badger
small.

## Tech stack

- **Frontend** — Svelte 5 (Runes) + Vite + TS. `nostr-tools` (events/signing/
  pool), `applesauce-*` (accounts/signers), `qrcode-generator`, `@dicebear/*`.
- **Relay** — Go: `khatru` + `relay29` (pinned to `master`) + `eventstore/badger`.
- **Audio** — YouTube IFrame API, conductor as the time authority (drift-corrected).
- **Lightning** — NIP-57 zaps via LNURL; WebLN / Alby Go / any wallet.
- **Hosting** — static frontend + relay behind Caddy on one box.

## Monorepo layout

```
frontend/   Svelte 5 client — player, sync, stage, queue, chat, zaps, profile
relay/      NIP-29 relay (Go) — membership, roles, moderation, content
design/     Branding assets (neon turntable, disco-ball)
tasks/      Working plan & lessons
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
CGO_ENABLED=0`) and shipped as a static binary. Deploy discipline: `go.mod` /
`go.sum` committed, **never** `go get` on the server, verify the feature is in
the binary after deploy.

See `CLAUDE.md` for the full data model, conductor rules and relay hardening.
