#!/usr/bin/env bash
set -euo pipefail

unit=${1:-zapclub-monitor.service}
recipient=${ZAPCLUB_ALERT_TO:-webmaster@kasper.digital}
state_dir=${ZAPCLUB_MONITOR_STATE_DIR:-/var/lib/zapclub-monitor}
msmtp_config=${MSMTP_CONFIG:-/etc/zapclub/msmtprc}
safe_unit=$(printf '%s' "$unit" | tr -c 'A-Za-z0-9._-' '_')
marker="$state_dir/alert-$safe_unit"
now=$(date +%s)

mkdir -p "$state_dir"
if [ -f "$marker" ]; then
  last=$(stat -c %Y "$marker")
  if [ $((now - last)) -lt 3600 ]; then
    echo "zapclub-alert suppressed: an alert for $unit was sent less than one hour ago"
    exit 0
  fi
fi

log=$(journalctl -u "$unit" -n 30 --no-pager -o cat 2>/dev/null | tail -30 || true)
[ -n "$log" ] || log="No journal entries available."

printf 'To: %s\nSubject: [zapclub.io] Monitor failure on %s\nContent-Type: text/plain; charset=utf-8\n\nUnit: %s\nHost: %s\nTime: %s\n\nRecent monitor output:\n%s\n' \
  "$recipient" "$(hostname)" "$unit" "$(hostname)" "$(date -Is)" "$log" \
  | msmtp --file="$msmtp_config" -t

touch "$marker"
echo "zapclub-alert sent: $unit to $recipient"
