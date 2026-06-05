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

## Status

Skeleton only — Go module and `main.go` land in Phase 1 (needs Go installed
locally first).
