# zapclub.io

<a href="https://zapclub.io">
  <img src="frontend/public/og.png" alt="zapclub.io — DJ and listen together" width="100%">
</a>

Zapclub is a collaborative live music club built on Nostr and Lightning. One
shared turntable keeps everyone in sync: members bring playlists, take the
stage, react to tracks and zap DJs directly. Identity is a Nostr key — no email
account is required.

- Web app: [zapclub.io](https://zapclub.io)
- NIP-29 relay: `wss://relay.zapclub.io`
- Source: [github.com/satoshidude/zapclub.io](https://github.com/satoshidude/zapclub.io)
- Interface language: English

## What Zapclub does

- Lists currently active clubs and makes all eligible clubs discoverable
  through search.
- Provides three shared stage slots for real DJs and an armed Auto DJ.
- Keeps YouTube playback synchronized from relay-authored timing events.
- Keeps an armed Auto DJ visibly on stage and falls back to its shuffled
  playlist when no real DJ is active.
- Provides member-only chat, presence and roster data alongside public,
  privacy-preserving member and listener counts.
- Adds floor reactions and a Vibemeter: reactions from other members build or
  lower DJ credibility, while the playing DJ cannot vote on their own track.
- Sends NIP-57 Lightning zaps directly to DJs and aggregates a public
  leaderboard.
- Supports open, closed, private and Lightning entry-fee clubs.
- Connects Telegram groups through a dedicated bridge bot.

## Architecture

```text
Browser (Svelte 5) ── NIP-07/46 ── Nostr signer
        │
        ├── HTTPS/WSS ── Caddy ─┬── static frontend
        │                       ├── NIP-29 relay + playback conductor
        │                       └── LNURL / NIP-05 proxy
        ├── YouTube IFrame API
        └── NIP-57 ── Lightning zaps to DJs

Telegram ── bridge bot ── NIP-29 relay
```

The Go relay is the authoritative shared-state component. It manages club
membership and roles, enforces access, rate and stage limits, and acts as the
always-on playback conductor. Browsers consume its state; they do not elect a
leader or write authoritative playback events.

Caddy terminates TLS and serves the built frontend. The LNURL/NIP-05 proxy and
Telegram bot are small adapters without application state.

## Access and privacy

Club metadata, stage state and playback are public. Chat, presence and the
member roster require NIP-42 authentication and current club membership; that
boundary also applies to history, direct event queries and live subscriptions.

Logged-out visitors use an ephemeral local key for schema- and rate-limited
listener heartbeats. Individual heartbeats remain server-side; the relay
publishes only an aggregate count. Member identities remain protected in the
roster; the relay exposes a separate signed total containing only club ID and
count. Profiles are read from public Nostr relays. Zap receipts are used only to
confirm the currently open invoice; profile and leaderboard history contains
exclusively Zapclub-marked zaps recorded by the Zapclub relay.

Playback state, public aggregates and DJ credibility are relay-authored.
Closed/private membership and paid entry are enforced by the relay;
administrative HTTP routes require fresh NIP-98 authorization.

## Playback model

Only the relay signs and stores `now_playing` and playback-log events. For each
club, the conductor indexes stage and queue events, orders DJs by their stable
stage timestamp, selects the next playable track and republishes timing state
roughly every 15 seconds. Clients correct playback when their drift exceeds
three seconds.

Tracks advance on duration, an authorized skip, a broken-track quorum or three
Vibemeter skips. The relay rejects both positive and negative Vibemeter votes
from the DJ whose track is playing. A real-DJ slot remains sticky for up to five
minutes after its last heartbeat. An armed Auto DJ owns one of the same three
stage slots and is always shown there. Its shuffled playlist drives playback
only while no real DJ is active and does not affect a person's DJ credibility.

## Nostr event model

All club content carries an `h` tag. NIP-29 metadata is queried separately by
`#d`; relay29 rejects subscriptions that mix metadata kinds with content kinds.

| Kinds | Purpose |
|---|---|
| `9000–9022`, `39000–39002` | NIP-29 administration, metadata, roles and members |
| `30100`, `1313` | Relay-authored playback state and log |
| `30101–30105` | Club configuration, stage, DJ queue, saved playlist and Auto DJ |
| `30106`, `30107`, `30111`, `30112` | Stage kick, authorized skip, Auto DJ control and public member count |
| `9`, `20100` | Member-only chat and ephemeral presence |
| `20101–20104` | Zap broadcast, broken-track report, floor reaction and Vibemeter |
| `20105`, `20106` | Anonymous listener heartbeat and relay-authored aggregate |
| `30078` | Relay-signed DJ credibility snapshot |
| `9734`, `9735` | NIP-57 zap request and receipt |

Nostr `created_at` values use seconds. Playback timing and client calculations
use milliseconds.

## Storage

BadgerDB is the Nostr event store and source of truth. SQLite maintains derived
conductor state, the offline-DJ played set and immutable club-owner lookups for
constant-time hot paths. Small JSON sidecars hold bans, listener analytics, DJ
credibility and the zap leaderboard.

All mutable data and secrets live outside release directories. The SQLite state
can be rebuilt from events, but persisting it avoids a cold-start scan.

## Repository

```text
frontend/      Svelte 5 / TypeScript client
relay/         Go NIP-29 relay and playback conductor
telegram-bot/  Telegram integration
deploy/        Project-specific Caddy, systemd, monitoring and backup files
```

## Production

Deployments use immutable, commit-addressed releases. Each candidate is built
and validated separately, activated through an atomic switch and checked through
the public protocols. A failed activation returns to the preceding verified
release.

Persistent state, secrets, backups and monitoring remain outside the release.
See [`deploy/README.md`](deploy/README.md) for the general deployment,
rollback, monitoring and backup architecture.
