#!/usr/bin/env bash
# Compatibility wrapper: backup logic now lives in the `dms` CLI.
# Usage stays the same: ./backup.sh  (delegates to `dms backup "$@"`)
set -euo pipefail

ROOT_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
DMS_BIN="$ROOT_DIR/dms"

if [[ ! -x "$DMS_BIN" ]]; then
  if command -v go >/dev/null 2>&1; then
    echo "dms binary not found, building it first..." >&2
    (cd "$ROOT_DIR/backend" && go build -o "$DMS_BIN" ./cmd/dms)
  else
    echo "dms binary missing and go is not installed; cannot back up." >&2
    exit 1
  fi
fi

exec "$DMS_BIN" backup "$@"
