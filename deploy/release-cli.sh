#!/bin/sh
# Release CLI contract v1. Kept locally; no cross-repository runtime dependency.
release_usage() {
    printf 'Usage: ./release.sh [--help|--dry-run|--check|--deploy] [--full]\n'
    printf 'Default: --deploy. Profile: %s (--full selects complete checks).\n' "$PROFILE"
    printf '%s\n' '--dry-run: local plan only; exits nonzero if deployment preflight fails.' \
      '--check: local checks/builds, including dirty development branches; no transfer.' \
      '--deploy: clean main, checks, exact commit upload, activation and verification.' \
      'Stage, commit and merge separately. No automatic Git staging or commits.'
}
release_fail() { printf '%s\n' "Release aborted: $*" >&2; exit 1; }
MODE=
for argument in "$@"; do
    case "$argument" in
        --help|--dry-run|--check|--deploy)
            [ -z "$MODE" ] || release_fail 'choose exactly one mode'
            MODE=$argument ;;
        --full) PROFILE=full ;;
        *) release_fail "unknown argument: $argument (see --help)" ;;
    esac
done
MODE=${MODE:---deploy}
if [ "$MODE" = --help ]; then release_usage; exit 0; fi
cd "$ROOT"
commit=$(git rev-parse --verify HEAD)
release_preflight() {
    [ "$(git branch --show-current)" = main ] || release_fail 'current branch is not main'
    [ -z "$(git status --porcelain)" ] || release_fail 'worktree is not clean'
    [ "$(git rev-parse HEAD)" = "$commit" ] || release_fail 'HEAD changed during checks'
}
if [ "$MODE" = --dry-run ]; then
    printf 'Project: %s\nCommit: %s\nProfile: %s\nTransport: %s\n' "$PROJECT" "$commit" "$PROFILE" "$TRANSPORT"
    printf 'Git remote: %s\nSSH target: %s\nHelper: %s\n' "$GIT_REMOTE" "$REMOTE" "$HELPER"
    printf '%s\n' 'No checks, uploads or server operations executed; this is not a release approval.'
    release_preflight
    release_requirements
    exit 0
fi
if [ "$MODE" = --deploy ]; then release_preflight; fi
release_requirements
release_checks_done() {
    if [ "$MODE" = --check ]; then
        printf 'Local checks completed: %s (%s). No deployment.\n' "$PROJECT" "$PROFILE"
        exit 0
    fi
    release_preflight
}
release_push() {
    release_preflight
    git push "$GIT_REMOTE" "$commit:refs/heads/main"
}
release_bundle() {
    # Keep the server's main-ref format, but reject concurrent branch movement.
    git bundle create "$BUNDLE" main
    [ "$(git bundle list-heads "$BUNDLE" refs/heads/main | awk 'NR == 1 {print $1}')" = "$commit" ] \
      || release_fail 'bundle revision differs from checked commit'
    release_preflight
}
