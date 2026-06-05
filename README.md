# zapclub.io

Collaborative, decentralized social music streaming. Nostr login instead of
accounts, real sats via zaps instead of points. No label platform, no tracking,
no central identity — a club that belongs to you.

> UI language is **English by default** with a DE/EN switcher.

## Monorepo layout

```
frontend/   Svelte 5 (Runes) + Vite + TS — client (player, sync, zap UI)
relay/      NIP-29 relay (Go, khatru + relay29, badger) — wss://relay.zapclub.io
design/     Branding assets (logo, neon palette)
tasks/      Working plan & lessons
```

## Architecture

Clubs are **NIP-29 relay-based groups**. The relay is the only central
component and the index (no separate DB). Up to 5 DJs share a stage; their
queues are interleaved round-robin into one club playlist. Exactly one
**conductor** (oldest active stage DJ; club owner overrides) writes the
`now_playing` event — clients compute drift-corrected playback position.

See `CLAUDE.md` for the full data model and the relay hardening notes.

## Develop

```sh
# Frontend
cd frontend && npm install && npm run dev

# Relay (requires Go)
cd relay && go run .   # serves on 127.0.0.1
```

## Deploy

Local → prod (no staging). Frontend served statically via Caddy on
`zapclub.io`; relay as a systemd service behind Caddy on `wss://relay.zapclub.io`.
Deploy discipline: `go.mod`/`go.sum` committed, never `go get` on the server,
verify the feature is in the binary after deploy.
