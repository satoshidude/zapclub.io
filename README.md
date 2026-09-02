# zapclub.io

[![zapclub.io — DJ and listen together](frontend/public/zapclub-banner.gif)](https://zapclub.io)

Decentralized social music streaming with Nostr identities, NIP-29 clubs,
Lightning zaps and synchronized YouTube playback.

- Frontend: `https://zapclub.io`
- Relay: `wss://relay.zapclub.io`
- UI language: English only

## Architecture

```text
Browser (Svelte 5) ── NIP-07/46 ── Nostr signer
        │
        ├── WebSocket ── NIP-29 relay (khatru + relay29 + BadgerDB)
        │                         └── conductor + SQLite hot-path state
        ├── YouTube IFrame API
        └── NIP-57 / NWC ── Lightning wallet
```

The relay is the only central component. It manages group membership and roles,
enforces write permissions and premium limits, and acts as the always-on playback
conductor. Profiles and zap receipts remain on public Nostr relays.

## Event model

All club content carries an `h` tag. NIP-29 metadata is queried separately by
`#d`; relay29 rejects subscriptions that mix metadata kinds with content kinds.

| Kind | Purpose |
|---|---|
| `9007`, `9002` | Create group, edit metadata |
| `9000`, `9001`, `9021`, `9022` | Roles, kick, join, leave |
| `39000–39002` | Relay-signed group metadata, admins, members |
| `30100` | Relay-authored `now_playing` state |
| `30101` | Owner-authored club/access configuration |
| `30102` | DJ stage heartbeat and stable `since` ordering |
| `30103` | Parameterized-replaceable queue per DJ and club |
| `30104` | Saved user playlist |
| `30107` | Authorized skip request |
| `1313` | Relay-authored playback log |
| `9`, `20100` | Chat and ephemeral presence |
| `20102`, `20104` | Broken-track report and mood vote |
| `9734`, `9735` | NIP-57 zap request and receipt |

Nostr `created_at` values use seconds. Playback timestamps (`started_at`,
`sent_at`) and all client calculations use milliseconds.

## Playback conductor

Only the relay signs and stores `30100` and `1313`. Browsers are consumers and
never participate in leader election. For each club, the conductor:

1. indexes active stage events and DJ queues;
2. interleaves playable tracks round-robin;
3. publishes a new `now_playing` event on track changes;
4. republishes it roughly every 15 seconds with a fresh `sent_at`;
5. advances on duration, authorized skip, broken-track quorum or mood threshold.

DJ order is determined by the persisted `since` value in the newest `30102`
event. A stage slot remains sticky for at most five minutes after the last
heartbeat. When no staged DJ has a playable queue, the stream returns to the
lobby track.

Round-robin uses a global position:

```text
djIndex    = pos % djCount
trackIndex = floor(pos / djCount)
```

Tracks marked `off` are masked. For offline DJs without a recent presence beat,
a persisted played-set prevents old queues from looping indefinitely.

## Client synchronization

Clients derive their target playback position from the relay heartbeat:

```text
offsetMs = sent_at - Date.now()
targetMs = Date.now() + offsetMs - started_at
```

The player loads at `targetMs`. Every five seconds it compares local playback
with the target and seeks when absolute drift exceeds three seconds.

## Storage

BadgerDB is the Nostr event store and source of truth. SQLite contains derived
state for constant-time hot-path lookups:

| Table | Data |
|---|---|
| `conductor_state` | Current position, video, DJ and start time per club |
| `played` | Offline-DJ played-set |
| `club_owners` | Immutable club creator lookup |
| `premium_cache` | Premium status with one-hour cache TTL |

`modernc.org/sqlite` is pure Go, so Linux binaries remain static without CGO.
The SQLite database can be rebuilt from events, but `SQLITE_PATH` should persist
across deployments to avoid a cold-start scan.

## Enforcement and security

- Group content is writable only by current members.
- `30100` and `1313` are accepted only from the relay key.
- Club, playlist and stage limits are enforced by relay hooks.
- Private and entry-fee clubs are relay-gated.
- NIP-42 authentication protects writes; public reads remain available.
- Admin endpoints require fresh NIP-98 authorization.
- Chat, search and HTTP endpoints are rate-limited.
- The relay listens on `127.0.0.1` behind Caddy/TLS.
- `RELAY_SECRET_KEY` is persistent, mode `600`, and never committed.

Important relay29 constraints:

- Keep relay29 pinned to the known-good `master` revision.
- Register `db.ReplaceEvent`, otherwise addressable events accumulate.
- Never mix kinds `39000–39003` with content kinds in one subscription.

## Repository layout

```text
frontend/      Svelte 5 / TypeScript client
relay/         Go NIP-29 relay and conductor
telegram-bot/  Telegram integration
workers/       Edge worker code
deploy/        Caddy and systemd configuration
```

## Local development

Requirements: Node.js/npm, Go and `yt-dlp`.

```sh
cd frontend
npm ci
npm run dev
```

```sh
cd relay
export RELAY_SECRET_KEY="$(openssl rand -hex 32)"
go run .
```

The frontend defaults to the live relay. The local relay listens on
`127.0.0.1:3334` unless configured otherwise.

## Test and build

```sh
cd frontend
npm test -- --run
npm run build
```

```sh
cd relay
go test ./...
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o zapclub-relay-linux .
```

## Installation and deployment

1. Build `frontend/dist` and serve it as the `zapclub.io` document root.
2. Install the relay binary and `deploy/zapclub-relay.service`.
3. Configure `RELAY_SECRET_KEY`, database paths and Lightning integration in
   the service environment.
4. Configure Caddy from `deploy/Caddyfile` for the frontend, WebSocket relay and
   HTTP endpoints.
5. Preserve BadgerDB and `conductor.db` across releases.
6. Start or restart the systemd service and verify the deployed binary/version.

Do not run `go get` or `go mod tidy` on the server. Dependencies are pinned in
`go.mod` and `go.sum`; build the release binary before deployment.
