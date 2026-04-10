#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
FRONTEND_DIR="$ROOT_DIR/frontend"
BACKEND_DIR="$ROOT_DIR/backend"
EMBED_DIST_DIR="$BACKEND_DIR/internal/http/web/dist"
OUTPUT_BINARY="$ROOT_DIR/personnel-management"

require_command() {
  if ! command -v "$1" >/dev/null 2>&1; then
    echo "Missing required command: $1" >&2
    exit 1
  fi
}

require_command go
require_command npm

cd "$FRONTEND_DIR"
if [[ ! -d node_modules ]]; then
  if [[ -f package-lock.json ]]; then
    npm ci
  else
    npm install
  fi
fi
npm run build

rm -rf "$EMBED_DIST_DIR"
cp -R "$FRONTEND_DIR/dist" "$EMBED_DIST_DIR"

cd "$BACKEND_DIR"
go mod tidy
go build -o "$OUTPUT_BINARY" ./cmd/server

echo "Build completed: $OUTPUT_BINARY"
