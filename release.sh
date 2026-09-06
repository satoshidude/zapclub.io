#!/bin/sh
set -eu
ROOT=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
PROJECT=zapclub
PROFILE=fast
REMOTE=${ZAPCLUB_DEPLOY_HOST:-sunnyhill.io}
GIT_REMOTE=origin
TRANSPORT=bundle
HELPER="/usr/local/sbin/vps-app-deploy zapclub"
release_requirements() { :; }
. "$ROOT/deploy/release-cli.sh"

version=$(node -p "require('./frontend/package.json').version")
if [ "$PROFILE" = full ]; then
    BUILD_DIR=$(mktemp -d "${TMPDIR:-/tmp}/zapclub-build.XXXXXX")
    trap 'rm -rf -- "$BUILD_DIR"' EXIT HUP INT TERM
    npm --prefix frontend ci
    npm --prefix frontend audit --audit-level=high
    npm --prefix frontend run check
    npm --prefix frontend test -- --run
    SOURCE_COMMIT="$commit" npm --prefix frontend run build
    (cd relay && go vet ./... && go test ./... && ./e2e.sh && CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags=-s -o "$BUILD_DIR/zapclub-relay" .)
    (cd telegram-bot && go vet ./... && go test ./... && CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags=-s -o "$BUILD_DIR/zapclub-telegram-bot" .)
    rm -rf -- "$BUILD_DIR"
    trap - EXIT HUP INT TERM
else
    if [ "${ZAPCLUB_FAST_SKIP_DEPS_INSTALL:-1}" != 1 ] || [ ! -d frontend/node_modules ]; then npm --prefix frontend ci; fi
    if [ "${ZAPCLUB_FAST_SKIP_AUDIT:-1}" != 1 ]; then npm --prefix frontend audit --audit-level=high; fi
    if [ "${ZAPCLUB_FAST_SKIP_CHECKS:-1}" != 1 ]; then npm --prefix frontend run check; fi
    SOURCE_COMMIT="$commit" npm --prefix frontend run build
fi
printf '%s\n' 'Server dispatcher still checks/builds the full stack and restarts relay, bot and LNURL.'
release_checks_done
REMOTE_BUNDLE=/home/webmaster/.deploy/zapclub-release.bundle
BUNDLE=$(mktemp "${TMPDIR:-/tmp}/zapclub-release.XXXXXX")
trap 'rm -f -- "$BUNDLE"' EXIT HUP INT TERM
release_push
release_bundle
scp "$BUNDLE" "$REMOTE:$REMOTE_BUNDLE"
ssh "$REMOTE" "sudo /usr/local/sbin/vps-app-deploy zapclub '$commit'"
ZAPCLUB_EXPECTED_COMMIT="$commit" ZAPCLUB_EXPECTED_VERSION="$version" node deploy/smoke.mjs
printf 'Zapclub release activated: %s\n' "$commit"
