# zapclub relay

NIP-29 relay-based groups relay for zapclub.io — `wss://relay.zapclub.io`.

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
- **`go.mod`/`go.sum` committed.** Never run `go get` / `go mod tidy` on the
  server (breaks `git pull`, silent no-op deploys). After deploy verify the
  feature is actually in the binary (`grep <feature> <binary>`), not just "build ok".

## Write protection

Only group members may write content events (kinds 9, 30100, 30102, 30103,
20100); the relay checks membership against the `h`-tag group. NIP-42 AUTH
challenge on connect; public clubs stay readable without AUTH.

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

Builds, boots, E2E green (Go 1.26). Roles: `owner` (creator) + `moderator`.
DJ/stage is a content event (30102), not a relay role. `entryfee.go` (sats
gate) intentionally not ported — not MVP. Next: deploy behind Caddy as a
systemd service on `wss://relay.zapclub.io`.
