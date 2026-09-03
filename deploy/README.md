# Production deployment

`sunnyhill` is the only production target. Deployments are immutable releases;
there is no mutable checkout and no in-place `git pull`.

## Layout

| Path | Purpose |
|---|---|
| `/srv/zapclub/releases/<commit>` | Complete checked-out and built release |
| `/srv/zapclub/current` | Symlink to the active release |
| `/srv/zapclub/current/bin` | Relay and Telegram bot binaries |
| `/var/lib/zapclub-relay` | BadgerDB, SQLite and derived JSON state |
| `/etc/zapclub` | Relay, Telegram and YouTube credentials |
| `/var/backups/zapclub` | Daily state and secrets backups |
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
- `relay/relay.env.example` → template for `/etc/zapclub/relay.env` (mode 600)
- `telegram-bot/.env.example` → template for `/etc/zapclub/telegram.env` (mode 600)

The main Caddyfile imports `/etc/caddy/zapclub.caddy`.

## Release procedure

1. Create `/srv/zapclub/releases/<commit>` from the exact Git commit.
2. Run `npm ci`, frontend checks/tests and the production build in the release.
3. Run Relay tests and build `bin/zapclub-relay` with `CGO_ENABLED=0`.
4. Run Telegram bot tests and build `bin/zapclub-telegram-bot` with `CGO_ENABLED=0`.
5. Validate `deploy/Caddyfile` and the systemd units before installation.
6. Atomically point `/srv/zapclub/current` at the new release.
7. Restart Relay, LNURL/NIP-05 and Telegram bot; reload Caddy only when its
   configuration changed.
8. Verify services, HTTP, WebSocket and the deployed commit before pruning old
   releases. Keep the immediately previous working release for rollback.

Rollback consists of atomically repointing `current` to the previous release and
restarting the three application services. Persistent state is never stored in a
release directory.

## Verification

```sh
systemctl is-active zapclub-relay zapclub-lnurlp zapclub-telegram-bot caddy
systemctl is-active zapclub-backup.timer
curl -fsS https://zapclub.io/ >/dev/null
curl -fsS https://zapclub.io/.well-known/nostr.json?name=satoshidude >/dev/null
curl -fsS https://relay.zapclub.io/ >/dev/null
readlink -f /srv/zapclub/current
```

The production stack consists only of Caddy, the relay/conductor, the
LNURL/NIP-05 proxy, the Telegram bot and the backup timer.
