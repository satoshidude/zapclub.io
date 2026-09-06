#!/bin/sh
set -eu

ROOT=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
REMOTE=${ZAPCLUB_DEPLOY_HOST:-sunnyhill.io}
REMOTE_BUNDLE=/home/webmaster/.deploy/zapclub-release.bundle
BUNDLE=$(mktemp "${TMPDIR:-/tmp}/zapclub-release.XXXXXX")
BUILD_DIR=$(mktemp -d "${TMPDIR:-/tmp}/zapclub-build.XXXXXX")
SKIP_CHECKS=${ZAPCLUB_FAST_SKIP_CHECKS:-0}
SKIP_AUDIT=${ZAPCLUB_FAST_SKIP_AUDIT:-0}
SKIP_DEPS=${ZAPCLUB_FAST_SKIP_DEPS_INSTALL:-0}

cleanup() {
	rm -f "$BUNDLE"
	rm -rf "$BUILD_DIR" "$ROOT/frontend/dist"
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
version=$(node -p "require('./frontend/package.json').version")

if [ "$SKIP_DEPS" = 1 ] && [ -d "$ROOT/frontend/node_modules" ]; then
	echo "Skipping dependency install (ZAPCLUB_FAST_SKIP_DEPS_INSTALL=1)."
else
	npm --prefix frontend ci
fi
if [ "$SKIP_AUDIT" = 1 ]; then
	echo "Skipping frontend audit (ZAPCLUB_FAST_SKIP_AUDIT=1)."
else
	npm --prefix frontend audit --audit-level=high
fi
if [ "$SKIP_CHECKS" = 1 ]; then
	echo "Skipping frontend checks (ZAPCLUB_FAST_SKIP_CHECKS=1)."
else
	npm --prefix frontend run check
fi
SOURCE_COMMIT="$commit" npm --prefix frontend run build

git push origin main

git bundle create "$BUNDLE" main
scp "$BUNDLE" "$REMOTE:$REMOTE_BUNDLE"
ssh "$REMOTE" "sudo /usr/local/sbin/vps-app-deploy zapclub '$commit'"

ZAPCLUB_EXPECTED_COMMIT="$commit" ZAPCLUB_EXPECTED_VERSION="$version" node deploy/smoke.mjs
printf 'Zapclub fast release activated: %s\n' "$commit"
