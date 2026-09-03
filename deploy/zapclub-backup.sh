#!/usr/bin/env bash
set -euo pipefail

DEST=/var/backups/zapclub
STAMP=$(date +%Y%m%d-%H%M%S)
OUT="$DEST/zapclub-$STAMP.tar.gz"
RELAY_WAS_ACTIVE=false
BOT_WAS_ACTIVE=false

restart_services() {
  if "$RELAY_WAS_ACTIVE"; then
    systemctl start zapclub-relay.service
  fi
  if "$BOT_WAS_ACTIVE"; then
    systemctl start zapclub-telegram-bot.service
  fi
}
trap restart_services EXIT

systemctl is-active --quiet zapclub-relay.service && RELAY_WAS_ACTIVE=true
systemctl is-active --quiet zapclub-telegram-bot.service && BOT_WAS_ACTIVE=true

if "$BOT_WAS_ACTIVE"; then
  systemctl stop zapclub-telegram-bot.service
fi
if "$RELAY_WAS_ACTIVE"; then
  systemctl stop zapclub-relay.service
fi

sqlite3 /var/lib/zapclub-relay/conductor.db \
  'PRAGMA wal_checkpoint(TRUNCATE); PRAGMA integrity_check;' \
  | tail -1 \
  | grep -qx 'ok'

install -d -m 0700 "$DEST"
tar -czf "$OUT" -C / var/lib/zapclub-relay etc/zapclub
chmod 0600 "$OUT"
tar -tzf "$OUT" >/dev/null

find "$DEST" -maxdepth 1 -type f -name 'zapclub-*.tar.gz' -printf '%T@ %p\n' \
  | sort -rn \
  | awk 'NR > 14 {sub(/^[^ ]+ /, ""); print}' \
  | xargs -r rm -f

printf '%s backup ok: %s (%s bytes)\n' "$(date -Is)" "$OUT" "$(stat -c %s "$OUT")"
