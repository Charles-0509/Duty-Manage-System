#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
BACKEND_DIR="$ROOT_DIR/backend"
BINARY_PATH="$ROOT_DIR/personnel-management"
ENV_FILE="$BACKEND_DIR/.env"
ENV_EXAMPLE_FILE="$BACKEND_DIR/.env.example"

port_in_use() {
  local port="$1"

  if command -v ss >/dev/null 2>&1; then
    ss -lHtn "sport = :$port" | grep -q .
    return
  fi

  if command -v netstat >/dev/null 2>&1; then
    netstat -ltn 2>/dev/null | awk '{print $4}' | grep -Eq "(^|:)$port$"
    return
  fi

  return 1
}

ensure_port_available() {
  local port="$1"

  if port_in_use "$port"; then
    echo "Port $port is already in use. Stop the conflicting process or change APP_PORT in backend/.env." >&2
    exit 1
  fi
}

require_go_if_needed() {
  if [[ -x "$BINARY_PATH" ]]; then
    return 0
  fi

  if ! command -v go >/dev/null 2>&1; then
    echo "go command not found. Run ./build.sh first or install Go." >&2
    exit 1
  fi
}

ensure_env_file() {
  if [[ -f "$ENV_FILE" ]]; then
    return 0
  fi

  if [[ ! -f "$ENV_EXAMPLE_FILE" ]]; then
    echo "Missing env template: $ENV_EXAMPLE_FILE" >&2
    exit 1
  fi

  cp "$ENV_EXAMPLE_FILE" "$ENV_FILE"
  echo "Created $ENV_FILE from $ENV_EXAMPLE_FILE"
  echo "Please update JWT_SECRET in $ENV_FILE before production use."
}

load_env_file() {
  if [[ ! -f "$ENV_FILE" ]]; then
    return 0
  fi

  set -a
  # shellcheck disable=SC1090
  . "$ENV_FILE"
  set +a
}

mkdir -p "$ROOT_DIR/data"

require_go_if_needed
ensure_env_file
load_env_file

PREFERRED_PORT="${APP_PORT:-3000}"
ensure_port_available "$PREFERRED_PORT"
export APP_PORT="$PREFERRED_PORT"
export DATABASE_PATH="${DATABASE_PATH:-../data/personnel.db}"
export PRIVATE_MEMBERS_PATH="${PRIVATE_MEMBERS_PATH:-../data/member.json}"
export JWT_SECRET="${JWT_SECRET:-please-change-me}"
export DEFAULT_ADMIN_PASSWORD="${DEFAULT_ADMIN_PASSWORD:-admin}"
export FIRST_MONDAY="${FIRST_MONDAY:-20260302}"
export GIN_MODE="${GIN_MODE:-release}"

if [[ "$JWT_SECRET" == "please-change-me" ]]; then
  echo "Warning: JWT_SECRET is still the default value. Update backend/.env before exposing this system."
fi

echo "Starting DMS on http://127.0.0.1:$APP_PORT"
echo "Database file: $DATABASE_PATH"
echo "Member file: $PRIVATE_MEMBERS_PATH"

cd "$BACKEND_DIR"
if [[ -x "$BINARY_PATH" ]]; then
  exec "$BINARY_PATH"
fi

exec go run ./cmd/server
