#!/usr/bin/env bash
# zapclub radio watchdog — verifies every ENABLED webradio actually delivers audio
# bytes, and heals the pipeline when it doesn't.
#
# Checks (per enabled club, from conductor.db radio_state):
#   probe the relay's listen endpoint on localhost for PROBE_S seconds; a healthy
#   stream (track, lobby loop or silent placeholder) delivers ~16 KB/s, so fewer
#   than MIN_BYTES means the station is wedged (dead pipe, no goroutine feeding it).
#
# Healing (only after FAIL_RUNS consecutive failing runs, rate-limited):
#   1. WARP proxy dead (socks5 check fails)  -> systemctl restart warp-svc
#   2. stream still dead                      -> systemctl restart zapclub-relay
#      (radio enabled-state survives restarts via SQLite radio_state; streams
#      auto-resume on the next conductor heartbeat)
#
# Installed as a systemd timer (zapclub-radio-watchdog.timer, every 2 min).
# All actions are logged to journald with tag "zapclub-watchdog".

set -u

DB=/var/lib/zapclub-relay/conductor.db
RELAY_URL=http://127.0.0.1:3334/radio
WARP_PROXY=socks5://127.0.0.1:40000
STATE_DIR=/run/zapclub-watchdog
PROBE_S=6
MIN_BYTES=8000          # 6 s at 128 kbit/s is ~96 KB; 8 KB is a generous floor
FAIL_RUNS=2             # consecutive failing runs before healing
RESTART_COOLDOWN_S=900  # max one relay restart per 15 min

log() { logger -t zapclub-watchdog "$1"; }

mkdir -p "$STATE_DIR"
FAILCOUNT_F="$STATE_DIR/failcount"
LASTRESTART_F="$STATE_DIR/last_relay_restart"
RESTARTS_F="$STATE_DIR/restart_streak"

clubs=$(sqlite3 "file:$DB?mode=ro" "SELECT club FROM radio_state WHERE enabled=1;" 2>/dev/null)
if [ -z "$clubs" ]; then
    rm -f "$FAILCOUNT_F"
    exit 0
fi

dead=""
for club in $clubs; do
    # Paused (enabled, but no DJs on stage) is intentionally silent — not dead.
    if curl -s -m 5 "$RELAY_URL/$club/info" 2>/dev/null | grep -q '"paused":true'; then
        continue
    fi
    # -w prints size_download even when curl exits 28 (timeout = healthy endless stream).
    bytes=$(curl -s -o /dev/null -m "$PROBE_S" -H "Accept: audio/mpeg" \
        -w '%{size_download}' "$RELAY_URL/$club" 2>/dev/null)
    bytes=${bytes:-0}
    if [ "$bytes" -lt "$MIN_BYTES" ]; then
        dead="$dead $club($bytes B)"
    fi
done

if [ -z "$dead" ]; then
    # All healthy — reset the failure AND restart streaks.
    rm -f "$FAILCOUNT_F" "$RESTARTS_F"
    exit 0
fi

count=$(( $(cat "$FAILCOUNT_F" 2>/dev/null || echo 0) + 1 ))
echo "$count" > "$FAILCOUNT_F"
log "dead stream(s):$dead (run $count/$FAIL_RUNS)"

if [ "$count" -lt "$FAIL_RUNS" ]; then
    exit 0  # tolerate a single bad probe (e.g. track-switch gap)
fi

# ── heal ──────────────────────────────────────────────────────────────────────

# 1. WARP proxy health: yt-dlp downloads need the socks5 exit.
warp_code=$(curl -s -o /dev/null -m 10 --proxy "$WARP_PROXY" \
    -w '%{http_code}' https://www.youtube.com/generate_204 2>/dev/null)
if [ "${warp_code:-000}" != "204" ]; then
    log "WARP proxy unhealthy (code=${warp_code:-none}) — restarting warp-svc"
    systemctl restart warp-svc
    sleep 8
fi

# 2. Stale yt-dlp is the most common cause of mass download failures (YouTube
#    changes break old extractor clients — "This video is not available" for
#    everything). -U is a fast no-op when already current.
if command -v yt-dlp >/dev/null 2>&1; then
    upd=$(yt-dlp -U 2>&1 | tail -1)
    log "yt-dlp: $upd"
fi

# 3. Relay restart, rate-limited with EXPONENTIAL backoff. State (radio_state,
#    conductor_state) is in SQLite, so a restart is safe: streams re-enable themselves
#    on startup. This also clears the in-memory negative download cache.
#    Backoff: if restarts don't heal (streak grows without a healthy run in between),
#    the cooldown doubles per attempt up to 6 h — a persistent root cause must not
#    turn into an endless 15-min restart loop.
now=$(date +%s)
last=$(cat "$LASTRESTART_F" 2>/dev/null || echo 0)
streak=$(cat "$RESTARTS_F" 2>/dev/null || echo 0)
cooldown=$RESTART_COOLDOWN_S
i=0
while [ "$i" -lt "$streak" ] && [ "$cooldown" -lt 21600 ]; do
    cooldown=$(( cooldown * 2 ))
    i=$(( i + 1 ))
done
[ "$cooldown" -gt 21600 ] && cooldown=21600
if [ $(( now - last )) -lt "$cooldown" ]; then
    log "relay restart skipped — cooldown ($(( now - last ))s < ${cooldown}s, streak=$streak)"
    exit 0
fi
echo "$now" > "$LASTRESTART_F"
echo "$(( streak + 1 ))" > "$RESTARTS_F"
log "restarting zapclub-relay to recover dead stream(s):$dead (streak=$(( streak + 1 )), next cooldown doubles)"
systemctl restart zapclub-relay
rm -f "$FAILCOUNT_F"
