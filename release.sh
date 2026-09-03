#!/bin/sh
set -eu

ROOT=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
REMOTE=${ZAPCLUB_DEPLOY_HOST:-sunnyhill.io}
REMOTE_BUNDLE=/home/webmaster/.deploy/zapclub-release.bundle
BUNDLE=$(mktemp "${TMPDIR:-/tmp}/zapclub-release.XXXXXX")
BUILD_DIR=$(mktemp -d "${TMPDIR:-/tmp}/zapclub-build.XXXXXX")

cleanup() {
	rm -f "$BUNDLE"
	rm -rf "$BUILD_DIR" "$ROOT/frontend/node_modules" "$ROOT/frontend/dist"
}
trap cleanup EXIT HUP INT TERM

cd "$ROOT"

[ "$(git branch --show-current)" = main ] || {
	echo "Deploy aborted: current branch is not main." >&2
	exit 1
}
[ -z "$(git status --porcelain)" ] || {
	echo "Deploy aborted: worktree is not clean." >&2
	exit 1
}

commit=$(git rev-parse HEAD)
npm --prefix frontend ci
npm --prefix frontend audit --audit-level=high
npm --prefix frontend run check
npm --prefix frontend test -- --run
SOURCE_COMMIT="$commit" npm --prefix frontend run build
(cd relay && go vet ./... && go test ./... && ./e2e.sh && CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags=-s -o "$BUILD_DIR/zapclub-relay" .)
(cd telegram-bot && go vet ./... && go test ./... && CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags=-s -o "$BUILD_DIR/zapclub-telegram-bot" .)
git push origin main
git bundle create "$BUNDLE" main
scp "$BUNDLE" "$REMOTE:$REMOTE_BUNDLE"
ssh "$REMOTE" "sudo /usr/local/sbin/vps-app-deploy zapclub '$commit'"
ZAPCLUB_EXPECTED_COMMIT="$commit" node deploy/smoke.mjs
printf 'Zapclub release activated: %s\n' "$commit"
