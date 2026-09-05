# Deployment architecture

Zapclub uses immutable, commit-addressed releases. A deployment never updates a
live checkout in place: each candidate is prepared separately, validated as a
complete unit and activated through one atomic switch.

This document describes the public deployment model. Hostnames, accounts,
filesystem paths, commands, credentials and the operational release procedure
are intentionally not documented here.

## Runtime components

```text
Internet ── TLS reverse proxy ─┬── static frontend
                              ├── Nostr relay + playback conductor
                              └── LNURL / NIP-05 proxy

Telegram ── bridge bot ── Nostr relay

Service manager ── application processes
Monitor         ── health checks and alerts
Backup service  ── persistent state and secrets
```

The reverse proxy is the only public entry point. It serves the frontend and
forwards application protocols to loopback-only services. The relay owns shared
club and playback state; the proxy adapter and Telegram bot remain stateless.

## Release lifecycle

1. A release is tied to one exact source commit.
2. The candidate is assembled in an isolated incoming directory.
3. Locked dependencies, source contents, generated assets and binaries are
   validated before activation.
4. Frontend, relay and integration checks run against the candidate rather than
   the active release.
5. The complete release is made immutable and its revision is recorded.
6. A single pointer switches all application services to the new release.
7. Service and public protocol checks gate successful activation.
8. If activation fails, the pointer returns to the preceding verified release.

The active and previous verified releases are sufficient for operation. Release
directories never contain mutable runtime state.

## State and secrets

Event storage, derived conductor state, operational sidecars and credentials
live outside the release tree. This separation allows application rollbacks
without copying or replacing current data.

Application processes run with an unprivileged identity and receive only the
files and supplementary access required for their role. Secrets are never part
of source control, build artifacts or release bundles.

## Health and rollback

Activation is considered successful only when all application services start
and the public frontend, metadata endpoint and Nostr relay respond as expected.
The published revision must match the activated release.

Rollback reactivates the preceding verified release atomically and restarts the
same service set. Persistent state remains in place, so rollback changes code
and assets without replacing live data.

## Monitoring and backups

Monitoring verifies services, release consistency, required artifacts,
database integrity, backup freshness, storage capacity and public endpoints.
Failures trigger rate-limited alerts; a successful run clears the alert throttle.

Backups cover all persistent application state and secrets. Writers are paused
where required, databases are checkpointed and verified, and completed archives
are checked before retention is applied.

## Repository scope

The files in this directory describe Zapclub-specific reverse-proxy routes,
service definitions, monitoring, alerting, backup and smoke-check behavior.
Host-wide infrastructure and the private operational procedure are maintained
outside this repository.
