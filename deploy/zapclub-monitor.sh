#!/usr/bin/env bash
set -euo pipefail

CURRENT=/srv/zapclub/current
RELEASES=/srv/zapclub/releases
STATE_DIR=/var/lib/zapclub-monitor
DATABASE=/var/lib/zapclub-relay/conductor.db
BACKUPS=/var/backups/zapclub
NODE=/usr/local/bin/node

fail() {
  printf 'zapclub-monitor failed: %s\n' "$*" >&2
  exit 1
}

for unit in caddy.service zapclub-relay.service zapclub-lnurlp.service zapclub-telegram-bot.service zapclub-backup.timer; do
  systemctl is-active --quiet "$unit" || fail "$unit is not active"
done

release=$(readlink -f "$CURRENT")
case "$release" in
  "$RELEASES"/*) ;;
  *) fail "current does not resolve below $RELEASES" ;;
esac

[ -f "$release/REVISION" ] || fail "release revision is missing"
[ "$(basename "$release")" = "$(cat "$release/REVISION")" ] || fail "release directory and revision differ"
[ -f "$release/frontend/dist/index.html" ] || fail "frontend build is missing"
[ -x "$release/bin/zapclub-relay" ] || fail "relay binary is missing"
[ -x "$release/bin/zapclub-telegram-bot" ] || fail "Telegram bot binary is missing"

quick_check=$(sqlite3 -readonly "$DATABASE" 'PRAGMA quick_check;')
[ "$quick_check" = ok ] || fail "SQLite quick_check returned: $quick_check"

find "$BACKUPS" -maxdepth 1 -type f -name 'zapclub-*.tar.gz' -mmin -1800 -size +0c -print -quit \
  | grep -q . || fail "no non-empty backup newer than 30 hours"

disk_used=$(df -P "$CURRENT" | awk 'NR == 2 {gsub(/%/, "", $5); print $5}')
[ "$disk_used" -lt 90 ] || fail "filesystem usage is ${disk_used}%"

attempt=1
while ! "$NODE" "$release/deploy/smoke.mjs"; do
  [ "$attempt" -lt 3 ] || fail "public smoke checks failed after $attempt attempts"
  attempt=$((attempt + 1))
  sleep 3
done

rm -f "$STATE_DIR/alert-zapclub-monitor.service"
date -Is > "$STATE_DIR/last-ok"
printf 'zapclub-monitor healthy: release=%s disk=%s%% backup_age<30h sqlite=ok\n' \
  "$(cat "$release/REVISION")" "$disk_used"
