#!/usr/bin/env sh
# Self-contained E2E for the zapclub relay. Builds the relay, boots an isolated THROWAWAY
# instance (free port, temp runtime, freshly generated keys, test superadmin), runs
# grouptest.mjs — including the admin NIP-98 ban/purge/replay/unban/delete-club tests —
# then tears everything down.
#
# Run:  cd relay && ./e2e.sh
set -e
cd "$(dirname "$0")"

TEST_ROOT=$(mktemp -d)
DB="$TEST_ROOT/db"
mkdir "$DB"
# ESM ignores NODE_PATH. Put the test script and its dependency link inside this run's
# private directory so concurrent release checks cannot remove or replace either one.
cp grouptest.mjs "$TEST_ROOT/grouptest.mjs"
ln -s "$(pwd)/../frontend/node_modules" "$TEST_ROOT/node_modules"
# Avoid the old fixed-port collision when two worktrees or tasks validate concurrently.
PORT=$(node -e 'const net=require("node:net");const s=net.createServer();s.listen(0,"127.0.0.1",()=>{process.stdout.write(String(s.address().port));s.close()})')

# Generate admin (superadmin) + relay keys. Output: "<adminSK> <adminPK> <relaySK> <relayPK>".
KEYS=$(cd "$TEST_ROOT" && node -e 'import("nostr-tools/pure").then(m=>{const sk=m.generateSecretKey();const r=m.generateSecretKey();process.stdout.write(Buffer.from(sk).toString("hex")+" "+m.getPublicKey(sk)+" "+Buffer.from(r).toString("hex")+" "+m.getPublicKey(r))})')
ASK=$(printf %s "$KEYS" | cut -d' ' -f1)
APK=$(printf %s "$KEYS" | cut -d' ' -f2)
RSK=$(printf %s "$KEYS" | cut -d' ' -f3)
RPK=$(printf %s "$KEYS" | cut -d' ' -f4)

go build -o "$TEST_ROOT/zc-e2e-relay" .
RELAY_SECRET_KEY="$RSK" RELAY_SUPERADMIN="$APK" RELAY_DB="$DB" RELAY_PORT="$PORT" \
  RELAY_SERVICE_URL="ws://127.0.0.1:$PORT" SQLITE_PATH="$DB/conductor.db" \
  RELAY_BANLIST="$DB/banned.json" RELAY_LISTENERS="$DB/listeners.json" \
  RELAY_LEADERBOARD="$DB/leaderboard.json" RELAY_CREDIBILITY="$DB/credibility.json" \
  "$TEST_ROOT/zc-e2e-relay" >"$TEST_ROOT/relay.log" 2>&1 &
RPID=$!
cleanup() {
  kill "$RPID" 2>/dev/null || true
  wait "$RPID" 2>/dev/null || true
  rm -rf "$TEST_ROOT"
}
trap cleanup EXIT INT TERM
sleep 1.5
if ! kill -0 "$RPID" 2>/dev/null; then
  cat "$TEST_ROOT/relay.log"
  exit 1
fi

RELAY_URL="ws://127.0.0.1:$PORT" ADMIN_URL="http://127.0.0.1:$PORT" ADMIN_SK="$ASK" RELAY_PK="$RPK" RELAY_SK="$RSK" \
  node "$TEST_ROOT/grouptest.mjs"
