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

require_node_major() {
  local required_major="$1"
  local node_version
  local node_major

  node_version="$(node -v 2>/dev/null || true)"
  if [[ -z "$node_version" ]]; then
    echo "node command not found. Install Node.js ${required_major}+ first." >&2
    exit 1
  fi

  node_major="${node_version#v}"
  node_major="${node_major%%.*}"

  if [[ "$node_major" -lt "$required_major" ]]; then
    echo "Node.js ${required_major}+ is required, but current version is ${node_version}." >&2
    echo "Recommended: install Node.js 20 LTS or newer, then rerun ./build.sh." >&2
    exit 1
  fi
}

require_command go
require_command npm
require_node_major 20

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
