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
    echo "Recommended: install Node.js 24 or newer, then rerun ./build.sh." >&2
    exit 1
  fi
}

configure_build_resources() {
  if [[ "${LOW_RESOURCE_BUILD:-1}" != "1" ]]; then
    return
  fi
  export GOMAXPROCS="${GOMAXPROCS:-1}"
  export GOFLAGS="${GOFLAGS:--p=1}"
  export GOGC="${GOGC:-50}"
  export NODE_OPTIONS="${NODE_OPTIONS:---max-old-space-size=384}"
}

install_frontend_dependencies() {
  if [[ ! -d node_modules || package-lock.json -nt node_modules/.package-lock.json ]]; then
    npm ci --no-audit --no-fund
  fi
}

require_command go
require_command npm
require_node_major 24
configure_build_resources

cd "$FRONTEND_DIR"
install_frontend_dependencies
npm run build

mkdir -p "$(dirname "$EMBED_DIST_DIR")"
rm -rf "$EMBED_DIST_DIR"
cp -R "$FRONTEND_DIR/dist" "$EMBED_DIST_DIR"

cd "$BACKEND_DIR"
go build -o "$OUTPUT_BINARY" ./cmd/server

DMS_BINARY="$ROOT_DIR/dms"
BUILD_COMMIT="$(git -C "$ROOT_DIR" rev-parse --short HEAD 2>/dev/null || echo unknown)"
# -ldflags 的值按空格拆分，日期必须使用无空格格式；main 包变量需用 main. 前缀
BUILD_DATE="$(date '+%Y-%m-%dT%H:%M:%S')"
go build -ldflags "-X main.buildCommit=$BUILD_COMMIT -X main.buildDate=$BUILD_DATE -X main.buildGoVersion=$(go env GOVERSION)" -o "$DMS_BINARY" ./cmd/dms

echo "Build completed: $OUTPUT_BINARY"
echo "Ops CLI: $DMS_BINARY"
