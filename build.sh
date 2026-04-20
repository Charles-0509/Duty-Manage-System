#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
FRONTEND_DIR="$ROOT_DIR/frontend"
BACKEND_DIR="$ROOT_DIR/backend"
EMBED_DIST_DIR="$BACKEND_DIR/internal/http/web/dist"
OUTPUT_BINARY="$ROOT_DIR/personnel-management"
LOW_RESOURCE_BUILD="${LOW_RESOURCE_BUILD:-auto}"
SKIP_FRONTEND_BUILD="${SKIP_FRONTEND_BUILD:-0}"

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

detect_total_memory_mb() {
  if [[ -r /proc/meminfo ]]; then
    awk '/MemTotal:/ { print int($2 / 1024) }' /proc/meminfo
    return 0
  fi

  if command -v sysctl >/dev/null 2>&1; then
    local bytes
    bytes="$(sysctl -n hw.memsize 2>/dev/null || true)"
    if [[ -n "$bytes" ]]; then
      echo $((bytes / 1024 / 1024))
      return 0
    fi
  fi

  echo 0
}

detect_cpu_count() {
  if command -v nproc >/dev/null 2>&1; then
    nproc
    return 0
  fi

  if command -v getconf >/dev/null 2>&1; then
    getconf _NPROCESSORS_ONLN
    return 0
  fi

  echo 1
}

append_flag_if_missing() {
  local current="$1"
  local flag="$2"

  if [[ " $current " == *" $flag "* ]]; then
    printf '%s' "$current"
    return 0
  fi

  if [[ -z "$current" ]]; then
    printf '%s' "$flag"
    return 0
  fi

  printf '%s %s' "$current" "$flag"
}

configure_low_resource_mode() {
  local total_memory_mb="$1"
  local cpu_count="$2"
  local enabled=0

  case "$LOW_RESOURCE_BUILD" in
    1|true|TRUE|yes|YES|on|ON)
      enabled=1
      ;;
    auto)
      if [[ "$total_memory_mb" -gt 0 && "$total_memory_mb" -le 1536 ]]; then
        enabled=1
      elif [[ "$cpu_count" -le 1 ]]; then
        enabled=1
      fi
      ;;
  esac

  if [[ "$enabled" -ne 1 ]]; then
    return 0
  fi

  export GOMAXPROCS="${GOMAXPROCS:-1}"
  export GOFLAGS="$(append_flag_if_missing "${GOFLAGS:-}" "-p=1")"
  export GOGC="${GOGC:-50}"
  export NODE_OPTIONS="${NODE_OPTIONS:---max-old-space-size=${NODE_MAX_OLD_SPACE_SIZE:-384}}"
  export npm_config_jobs="${npm_config_jobs:-1}"

  echo "Low-resource build mode enabled (memory: ${total_memory_mb}MB, cpu: ${cpu_count})."
  echo "Using GOMAXPROCS=$GOMAXPROCS GOFLAGS=$GOFLAGS NODE_OPTIONS=$NODE_OPTIONS"
}

sync_frontend_dist() {
  local source_dist="$FRONTEND_DIR/dist"

  if [[ ! -d "$source_dist" ]]; then
    echo "Frontend dist not found: $source_dist" >&2
    echo "Run frontend build first or set SKIP_FRONTEND_BUILD=0." >&2
    exit 1
  fi

  rm -rf "$EMBED_DIST_DIR"
  cp -R "$source_dist" "$EMBED_DIST_DIR"
}

TOTAL_MEMORY_MB="$(detect_total_memory_mb)"
CPU_COUNT="$(detect_cpu_count)"

require_command go

configure_low_resource_mode "$TOTAL_MEMORY_MB" "$CPU_COUNT"

if [[ "$SKIP_FRONTEND_BUILD" != "1" ]]; then
  require_command npm
  require_node_major 20

  cd "$FRONTEND_DIR"
  if [[ ! -d node_modules ]]; then
    if [[ -f package-lock.json ]]; then
      npm ci --no-audit --no-fund
    else
      npm install --no-audit --no-fund
    fi
  fi
  npm run build
else
  echo "Skipping frontend build because SKIP_FRONTEND_BUILD=1."
fi

sync_frontend_dist

cd "$BACKEND_DIR"
go build -o "$OUTPUT_BINARY" ./cmd/server

echo "Build completed: $OUTPUT_BINARY"
