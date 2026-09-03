# Production operations and release plan

`sunnyhill` is the only production target. Deployments are immutable releases;
there is no mutable checkout and no in-place `git pull`.

From a clean local `main`, `./release.sh` runs the project checks, pushes the
reviewed commit, transfers a Git bundle through the unprivileged `webmaster`
account and invokes the root-owned `vps-app-deploy` validator. This document is
the authoritative runbook; retired hosts and former streaming components are
not part of the production design.

## Layout

| Path | Purpose |
|---|---|
| `/srv/zapclub/releases/<commit>` | Complete checked-out and built release |
| `/srv/zapclub/current` | Symlink to the active release |
| `/srv/zapclub/current/bin` | Relay and Telegram bot binaries |
| `/var/lib/zapclub-relay` | BadgerDB, SQLite and derived JSON state |
| `/etc/zapclub` | Relay, Telegram and YouTube credentials |
| `/var/backups/zapclub` | Daily state and secrets backups |
| `/var/lib/zapclub-monitor` | Last successful check and alert throttle state |
| `/etc/caddy/zapclub.caddy` | Installed copy of `deploy/Caddyfile` |

All application processes run as `zapclub`. Caddy is the only public listener;
the relay and LNURL/NIP-05 proxy bind to loopback ports 3334 and 3335.

## Managed files

- `Caddyfile` → `/etc/caddy/zapclub.caddy`
- `zapclub-relay.service` → `/etc/systemd/system/`
- `zapclub-lnurlp.service` → `/etc/systemd/system/`
- `zapclub-telegram-bot.service` → `/etc/systemd/system/`
- `zapclub-backup.service` and `.timer` → `/etc/systemd/system/`
- `zapclub-backup.sh` → `/usr/local/sbin/zapclub-backup.sh` (mode 755)
- `zapclub-monitor.service` and `.timer` → `/etc/systemd/system/`
- `zapclub-alert@.service` → `/etc/systemd/system/`
- `zapclub-monitor.sh` and `zapclub-alert.sh` → `/usr/local/sbin/` (mode 755)
- `smoke.mjs` stays in the release and is used by both release and monitoring
- `relay/relay.env.example` → template for `/etc/zapclub/relay.env` (mode 600)
- `telegram-bot/.env.example` → template for `/etc/zapclub/telegram.env` (mode 600)
- `/etc/zapclub/msmtprc` contains the root-only SMTP configuration for alerts

The main Caddyfile imports `/etc/caddy/zapclub.caddy`.

## Release procedure

1. `release.sh` requires a clean local `main`, installs the locked frontend
   dependencies and runs npm audit, frontend check/test/build, Go vet/test,
   Relay E2E and static production builds.
2. It pushes the exact commit to `origin/main`, creates a Git bundle and uploads
   that bundle through `webmaster`.
3. The root-owned validator independently verifies the bundle/commit, rejects
   secrets, databases and symlinks, repeats build and test checks as `zapclub`
   and records the full commit in `REVISION`.
4. The validator atomically points `current` at the built release, restarts the
   three application services and rolls back on a failed service or public
   health check.
5. `release.sh` finishes with public frontend, NIP-05, relay HTTP and Nostr
   WebSocket/EOSE checks. The release is complete only if these pass.
6. Keep only the active release and the immediately previous working release.
   The previous one is operational rollback state, not an inactive deployment.

Rollback consists of atomically repointing `current` to the previous release and
restarting the three application services. Persistent state is never stored in a
release directory.

## Verification

```sh
systemctl is-active zapclub-relay zapclub-lnurlp zapclub-telegram-bot caddy
systemctl is-active zapclub-backup.timer zapclub-monitor.timer
systemctl start zapclub-monitor.service
systemctl show zapclub-monitor.service -p Result -p ExecMainStatus
journalctl -u zapclub-monitor.service -n 10 --no-pager
readlink -f /srv/zapclub/current
cat /srv/zapclub/current/REVISION
```

The production stack consists only of Caddy, the relay/conductor, the
LNURL/NIP-05 proxy, the Telegram bot, the backup timer and the monitor timer.

## Monitoring and backups

`zapclub-monitor.timer` runs every five minutes. The check fails unless all
application services and the backup timer are active, `current` and `REVISION`
match, required build artifacts exist, SQLite reports `ok`, a non-empty backup
is newer than 30 hours, disk use is below 90 percent and all public smoke checks
pass. Public checks are retried three times to suppress brief network glitches.

`zapclub-alert@.service` sends the latest monitor journal by mail immediately on
failure and suppresses repeat mail for one hour. A successful monitor run clears
the throttle so a later independent incident alerts immediately. Alert SMTP
credentials stay in `/etc/zapclub/msmtprc`; they are never stored in a release.

`zapclub-backup.timer` runs daily at 03:40 with up to ten minutes randomized
delay. It stops writers, checkpoints and verifies SQLite, archives all persistent
state and `/etc/zapclub`, verifies the archive and retains the latest 14 backups.

## System configuration changes

Application releases never execute repository files as root. Changes under
`deploy/` therefore require an explicit root review: validate the Caddy fragment
with `caddy validate`, validate units with `systemd-analyze verify`, install only
the managed files listed above, run `systemctl daemon-reload`, and restart or
reload only affected units. Enable both timers with:

```sh
systemctl enable --now zapclub-backup.timer zapclub-monitor.timer
```
