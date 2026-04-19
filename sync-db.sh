#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
BIN="$ROOT/db-sync"
PUSH_BIN="$ROOT/db-push"

if [[ -n "${SYNC_TARGET_URL:-}" ]]; then
  if [[ -x "$PUSH_BIN" ]]; then
    exec "$PUSH_BIN"
  fi

  cd "$ROOT/backend"
  exec go run ./cmd/dbpush
fi

if [[ -x "$BIN" ]]; then
  exec "$BIN"
fi

if [[ -n "${SYNC_SOURCE_URL:-}" ]]; then
  cd "$ROOT/backend"
  exec go run ./cmd/dbsync
fi

echo "Either SYNC_TARGET_URL or SYNC_SOURCE_URL must be set." >&2
exit 1
