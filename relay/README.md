# Zapclub relay

Private NIP-29 group relay and playback conductor for Zapclub —
`wss://relay.zapclub.io`.

Built on **khatru + relay29** (Go, badger eventstore). Listens only on
`127.0.0.1`; exposed via Caddy reverse proxy (TLS, WebSocket).

## Hard-won rules (do not regress)

- **Pin `relay29` to `master`**, not tag v0.5.1 — v0.5.1 inverts `open`/`closed`,
  which breaks auto-join of open clubs.
- **Register the ReplaceEvent handler:**
  `relay.ReplaceEvent = append(relay.ReplaceEvent, db.ReplaceEvent)` — otherwise
  addressable events (kind 30100) accumulate instead of replacing → DB bloat.
  Only visible via E2E test, not code review.
- **Never mix metadata kinds (39000–39003) with other kinds in one subscription** —
  the relay rejects it. Use two separate subs.
- **`go.mod`/`go.sum` committed.** Never run `go get` / `go mod tidy` during a
  release. Build from the pinned module files before switching the active release.
- **Keep Khatru on the pinned final commit.** Its `PreventBroadcast` continues with
  the next listener instead of returning after the first blocked connection. The
  local `relay29init.go` adapter bridges its delivery-count return value to the
  archived relay29 interface.

## Write protection

Only group members may write club content; the relay checks membership against
the `h`-tag group. The sole exception is the empty, anonymous `20105` listener
heartbeat; its relay-signed `20106` aggregate exposes only the count. The public
relay-signed `30112` member aggregate likewise exposes only a club ID and count,
never roster identities. `30100`, `1313`, `20106`, `30112` and NIP-78
credibility snapshots are relay-authored only. NIP-42 AUTH runs on connect.
Public club metadata, playback, stage and aggregate counts remain readable
without AUTH, while kind `9`, presence and `39002` are served only to
authenticated current members. The check applies to history, direct event-ID
queries and every live push, so leave/kick also revokes an already-open
subscription.

Vibemeter kind `20104` is ephemeral and membership-gated. The relay rejects
Banger and Skip reactions from the DJ whose track is currently playing. Other
members share one relay-enforced reaction every 10 seconds, including repeated
reactions from the same member. The conductor caps each track at five banger
clicks, awards one point per click, advances after three skips with a score of
minus one, and stores the settled DJ score in
`credibility.json`; its current aggregate is mirrored as a relay-signed kind
`30078` event with `h=zapclub-credibility` and
`d=zapclub:credibility:<pubkey>`.

## DJ leaderboard and Zap history

Public `GET /leaderboard` ranks only DJs with at least one rank-eligible human
track. Its source is the same durable `credibility.json` aggregate that backs
the signed kind `30078` snapshots; Auto-DJ tracks and Lightning payments are
excluded. For `tracks=T` and canonical Vibemeter `score=V`, the relay calculates:

```text
earned   = clamp(T + V, 0, 6 × T)
DJ SCORE = 100 × earned / (6 × (T + 10))
```

A naturally finished track supplies one play point plus zero to five accepted
bangers; a community-skipped track supplies zero. Manual and broken-track skips
are persisted as handled for deduplication but add neither a track nor score.
The ten-track prior makes ten songs 50% experienced and prevents tiny samples
or unreviewed volume from dominating. The JSON `score` is an integer in tenths
(`333` means `33.3`) and is returned with `tracks`, `bangers`, `skipped` and the
canonical `vibeScore`.
Ranks compare the unrounded formula, then the larger settled sample, then the
pubkey for a deterministic order. An outgoing real-DJ track is settled before
an Auto-DJ takeover.

The same response includes `topTracks`, the ten settled human-DJ performances
with the most accepted Banger votes. Every row carries `title`, `videoId`,
`club`, `dj`, `bangers`, `skipped` and `startedAt`, so one play is attributable
to its club and DJ without publishing voter identities. Equal Banger totals are
ordered by the newer play. The durable performance history is capped at 100;
tracks with no Banger vote are omitted from the public Top 10. Existing
credibility files remain compatible, but track-level history begins only after
a relay version that records these performances is active. Plays in unlisted
private clubs continue to affect the DJ's aggregate credibility but never add
an attributable public track row.

Private `GET /zaps/received` remains a separate payment history containing only
zaps recorded on Zapclub. Club zaps enter through kind `20101`; browser-confirmed
profile and guest zaps are submitted to `POST /zaps` with their signed kind
`9734` request carrying `client=zapclub.io`. The request signature binds sender,
recipient and amount. The private sender list additionally requires a fresh
NIP-98 signature from that recipient.

## Secrets

`RELAY_SECRET_KEY` lives in `relay.env` (mode 600), never in the repo. Keep it
persistent for idempotent deploys. See `relay.env.example`.

## Run locally

```sh
export RELAY_SECRET_KEY=$(openssl rand -hex 32)
export RELAY_DB=$(mktemp -d) RELAY_PORT=3334
go run .                      # serves on 127.0.0.1:3334
```

## E2E test (self-contained)

```sh
cd relay && ./e2e.sh     # builds + boots a throwaway relay, runs grouptest.mjs, tears down
```

`e2e.sh` generates fresh keys, boots a temp-DB relay with `RELAY_SUPERADMIN` set to the
test admin key, and runs the full suite — **including the admin NIP-98 path**: ban (+
event purge), banned-member write rejection, NIP-98 token replay → 401, unban, and
delete-club (metadata gone). No manual setup. Expect `ALL PASSED`.

## E2E test (manual, against a running relay)

`grouptest.mjs` verifies the lessons code review can't catch: open-club
auto-join, now_playing (30100) ReplaceEvent dedup, and non-member write
rejection (plus the admin tests when `ADMIN_SK`/`ADMIN_URL` are set). Needs
`nostr-tools` reachable (ESM ignores `NODE_PATH`, so symlink `node_modules` to
an install that has it):

```sh
ln -sfn <path-to>/node_modules node_modules
RELAY_URL=ws://127.0.0.1:3334 node grouptest.mjs   # expect "ALL PASSED"
rm node_modules
```

Note: content events (30100/30102/30103/9) are queryable **only by `#h`**
(the group), not by `#d` — the client reads `{kinds:[…],"#h":[club]}` and
selects the `d`-address client-side.

## Status

Live at `wss://relay.zapclub.io`.

Roles: `owner` (creator) + `moderator`. DJ/stage is a content event (30102),
not a relay role.

Relay-enforced gates cover paid entry (`entryfee.go`), club count
(`clubcap.go`), owner-only Auto DJ (`autodjgate.go`) and the three shared stage
slots in `conductor.go`. An armed Auto DJ permanently occupies one of those
slots, even while real DJs have playback priority. Closed-club membership is
enforced by relay29.
