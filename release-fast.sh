#!/bin/sh
set -eu
ROOT=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
# Compatibility entry point; fast is now the root release default.
exec "$ROOT/release.sh" "$@"
