#!/usr/bin/env sh
# Self-contained E2E for the zapclub relay. Builds the relay, boots a THROWAWAY instance
# (temp DB, freshly generated keys, RELAY_SUPERADMIN = the test admin key), runs
# grouptest.mjs — including the admin NIP-98 ban/purge/replay/unban/delete-club tests —
# then tears everything down.
#
# Run:  cd relay && ./e2e.sh
set -e
cd "$(dirname "$0")"

PORT=3344
# ESM ignores NODE_PATH → symlink a node_modules that has nostr-tools (the frontend's).
ln -sfn ../frontend/node_modules node_modules

# Generate admin (superadmin) + relay keys. Output: "<adminSK> <adminPK> <relaySK> <relayPK>".
KEYS=$(node -e 'import("nostr-tools/pure").then(m=>{const sk=m.generateSecretKey();const r=m.generateSecretKey();process.stdout.write(Buffer.from(sk).toString("hex")+" "+m.getPublicKey(sk)+" "+Buffer.from(r).toString("hex")+" "+m.getPublicKey(r))})')
ASK=$(printf %s "$KEYS" | cut -d' ' -f1)
APK=$(printf %s "$KEYS" | cut -d' ' -f2)
RSK=$(printf %s "$KEYS" | cut -d' ' -f3)
RPK=$(printf %s "$KEYS" | cut -d' ' -f4)

DB=$(mktemp -d)
go build -o /tmp/zc-e2e-relay .
RELAY_SECRET_KEY="$RSK" RELAY_SUPERADMIN="$APK" RELAY_DB="$DB" RELAY_PORT="$PORT" \
  RELAY_SERVICE_URL="ws://127.0.0.1:$PORT" \
  /tmp/zc-e2e-relay >/tmp/zc-e2e-relay.log 2>&1 &
RPID=$!
trap 'kill $RPID 2>/dev/null; rm -rf "$DB" node_modules /tmp/zc-e2e-relay' EXIT
sleep 1.5

RELAY_URL="ws://127.0.0.1:$PORT" ADMIN_URL="http://127.0.0.1:$PORT" ADMIN_SK="$ASK" RELAY_PK="$RPK" RELAY_SK="$RSK" \
  node grouptest.mjs
