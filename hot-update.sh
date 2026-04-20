#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
BACKEND_DIR="$ROOT_DIR/backend"
ENV_FILE="$BACKEND_DIR/.env"
ENV_EXAMPLE_FILE="$BACKEND_DIR/.env.example"
RUNTIME_DIR="${HOT_RUNTIME_DIR:-$ROOT_DIR/.hot-runtime}"
BLUE_DIR="$RUNTIME_DIR/blue"
GREEN_DIR="$RUNTIME_DIR/green"
ACTIVE_SLOT_FILE="$RUNTIME_DIR/active-slot"
STATE_FILE="$RUNTIME_DIR/active-backend.txt"
PROXY_PID_FILE="$RUNTIME_DIR/hot-proxy.pid"
PROXY_BINARY="$RUNTIME_DIR/hot-proxy"
LOG_DIR="$RUNTIME_DIR/logs"

COMMAND="${1:-deploy}"
HOT_SLOT_BLUE_PORT="${HOT_SLOT_BLUE_PORT:-18081}"
HOT_SLOT_GREEN_PORT="${HOT_SLOT_GREEN_PORT:-18082}"
HOT_SWITCH_DRAIN_SECONDS="${HOT_SWITCH_DRAIN_SECONDS:-5}"
PYTHON_BIN=""

require_command() {
  if ! command -v "$1" >/dev/null 2>&1; then
    echo "Missing required command: $1" >&2
    exit 1
  fi
}

resolve_python_bin() {
  if command -v python3 >/dev/null 2>&1; then
    PYTHON_BIN="python3"
    return 0
  fi

  if command -v python >/dev/null 2>&1; then
    PYTHON_BIN="python"
    return 0
  fi

  echo "Missing required command: python3 (or python)" >&2
  exit 1
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
  set -a
  # shellcheck disable=SC1090
  . "$ENV_FILE"
  set +a
}

resolve_path() {
  local base_dir="$1"
  local raw_path="$2"

  if [[ "$raw_path" = /* ]]; then
    printf '%s\n' "$raw_path"
    return 0
  fi

  (
    cd "$base_dir"
    pwd -P
  ) >/dev/null

  (
    cd "$base_dir"
    "$PYTHON_BIN" - "$raw_path" <<'PY'
import os
import sys
print(os.path.abspath(sys.argv[1]))
PY
  )
}

pid_is_running() {
  local pid_file="$1"
  if [[ ! -f "$pid_file" ]]; then
    return 1
  fi

  local pid
  pid="$(tr -d '[:space:]' < "$pid_file")"
  if [[ -z "$pid" ]]; then
    return 1
  fi

  kill -0 "$pid" >/dev/null 2>&1
}

wait_for_process_exit() {
  local pid="$1"
  local timeout_seconds="$2"
  local waited=0

  while kill -0 "$pid" >/dev/null 2>&1; do
    if [[ "$waited" -ge "$timeout_seconds" ]]; then
      return 1
    fi
    sleep 1
    waited=$((waited + 1))
  done

  return 0
}

ensure_runtime_dirs() {
  mkdir -p "$BLUE_DIR" "$GREEN_DIR" "$LOG_DIR"
}

active_slot() {
  if [[ -f "$ACTIVE_SLOT_FILE" ]]; then
    tr -d '[:space:]' < "$ACTIVE_SLOT_FILE"
    return 0
  fi
  printf '\n'
}

slot_dir() {
  case "$1" in
    blue) printf '%s\n' "$BLUE_DIR" ;;
    green) printf '%s\n' "$GREEN_DIR" ;;
    *) echo "unknown slot: $1" >&2; exit 1 ;;
  esac
}

slot_port() {
  case "$1" in
    blue) printf '%s\n' "$HOT_SLOT_BLUE_PORT" ;;
    green) printf '%s\n' "$HOT_SLOT_GREEN_PORT" ;;
    *) echo "unknown slot: $1" >&2; exit 1 ;;
  esac
}

inactive_slot() {
  case "$(active_slot)" in
    blue) printf 'green\n' ;;
    green) printf 'blue\n' ;;
    *) printf 'blue\n' ;;
  esac
}

slot_pid_file() {
  printf '%s/app.pid\n' "$(slot_dir "$1")"
}

slot_binary_path() {
  printf '%s/personnel-management\n' "$(slot_dir "$1")"
}

write_active_slot() {
  local slot="$1"
  printf '%s\n' "$slot" > "$ACTIVE_SLOT_FILE"
}

write_state_file() {
  local upstream_url="$1"
  local temp_file="$STATE_FILE.tmp"
  printf '%s\n' "$upstream_url" > "$temp_file"
  mv "$temp_file" "$STATE_FILE"
}

health_check() {
  local port="$1"
  curl -fsS --max-time 3 "http://127.0.0.1:${port}/health" >/dev/null
}

wait_for_health() {
  local port="$1"
  local timeout_seconds="${2:-60}"
  local waited=0

  while ! health_check "$port"; do
    if [[ "$waited" -ge "$timeout_seconds" ]]; then
      return 1
    fi
    sleep 1
    waited=$((waited + 1))
  done

  return 0
}

build_proxy() {
  mkdir -p "$RUNTIME_DIR"
  (
    cd "$BACKEND_DIR"
    go build -o "$PROXY_BINARY" ./cmd/hotproxy
  )
}

start_proxy() {
  if pid_is_running "$PROXY_PID_FILE"; then
    return 0
  fi

  build_proxy

  local bind_port="${APP_PORT:-3000}"
  local bind_addr="${HOT_PROXY_BIND_ADDR:-:${bind_port}}"
  HOT_PROXY_BIND_ADDR="$bind_addr" HOT_PROXY_STATE_PATH="$STATE_FILE" \
    "$PROXY_BINARY" > "$LOG_DIR/hot-proxy.log" 2>&1 &
  echo $! > "$PROXY_PID_FILE"
  sleep 1
  if ! pid_is_running "$PROXY_PID_FILE"; then
    echo "failed to start hot proxy" >&2
    exit 1
  fi
}

stop_proxy() {
  if ! pid_is_running "$PROXY_PID_FILE"; then
    rm -f "$PROXY_PID_FILE"
    return 0
  fi

  local pid
  pid="$(tr -d '[:space:]' < "$PROXY_PID_FILE")"
  kill -TERM "$pid" >/dev/null 2>&1 || true
  if ! wait_for_process_exit "$pid" 15; then
    kill -KILL "$pid" >/dev/null 2>&1 || true
  fi
  rm -f "$PROXY_PID_FILE"
}

build_slot_binary() {
  local slot="$1"
  local output
  output="$(slot_binary_path "$slot")"
  mkdir -p "$(slot_dir "$slot")"
  OUTPUT_BINARY_PATH="$output" "$ROOT_DIR/build.sh"
}

start_slot() {
  local slot="$1"
  local port="$2"
  local binary_path
  local pid_file

  binary_path="$(slot_binary_path "$slot")"
  pid_file="$(slot_pid_file "$slot")"

  if [[ ! -x "$binary_path" ]]; then
    echo "slot binary missing: $binary_path" >&2
    exit 1
  fi

  stop_slot "$slot" || true

  APP_PORT="$port" \
  DATABASE_PATH="$ABS_DATABASE_PATH" \
  PRIVATE_MEMBERS_PATH="$ABS_MEMBER_PATH" \
  JWT_SECRET="$JWT_SECRET" \
  DEFAULT_ADMIN_PASSWORD="$DEFAULT_ADMIN_PASSWORD" \
  FIRST_MONDAY="$FIRST_MONDAY" \
  GIN_MODE="${GIN_MODE:-release}" \
  SYNC_ENABLED="${SYNC_ENABLED:-false}" \
  SYNC_TOKEN="${SYNC_TOKEN:-}" \
  "$binary_path" > "$LOG_DIR/${slot}.log" 2>&1 &

  echo $! > "$pid_file"
}

slot_process_running() {
  pid_is_running "$(slot_pid_file "$1")"
}

stop_slot() {
  local slot="$1"
  local pid_file
  pid_file="$(slot_pid_file "$slot")"

  if ! pid_is_running "$pid_file"; then
    rm -f "$pid_file"
    return 0
  fi

  local pid
  pid="$(tr -d '[:space:]' < "$pid_file")"
  kill -TERM "$pid" >/dev/null 2>&1 || true
  if ! wait_for_process_exit "$pid" 20; then
    kill -KILL "$pid" >/dev/null 2>&1 || true
  fi
  rm -f "$pid_file"
}

deploy_slot() {
  local next_slot="$1"
  local next_port
  next_port="$(slot_port "$next_slot")"

  echo "Building release for slot $next_slot on port $next_port"
  build_slot_binary "$next_slot"

  echo "Starting slot $next_slot"
  start_slot "$next_slot" "$next_port"

  if ! wait_for_health "$next_port" 60; then
    echo "new slot $next_slot failed health check" >&2
    stop_slot "$next_slot" || true
    exit 1
  fi
}

bootstrap_stack() {
  local first_slot="blue"
  local first_port
  first_port="$(slot_port "$first_slot")"

  deploy_slot "$first_slot"
  write_state_file "http://127.0.0.1:${first_port}"
  start_proxy
  write_active_slot "$first_slot"
  echo "Hot-update stack started. Active slot: $first_slot"
}

ensure_stack_started() {
  local current_slot current_port
  current_slot="$(active_slot)"

  if [[ -z "$current_slot" ]]; then
    bootstrap_stack
    return 0
  fi

  current_port="$(slot_port "$current_slot")"

  if ! slot_process_running "$current_slot"; then
    echo "Active slot $current_slot is not running. Starting it on port $current_port."
    if [[ ! -x "$(slot_binary_path "$current_slot")" ]]; then
      echo "Binary for slot $current_slot not found. Rebuilding it."
      build_slot_binary "$current_slot"
    fi
    start_slot "$current_slot" "$current_port"
  fi

  if ! wait_for_health "$current_port" 60; then
    echo "Active slot $current_slot failed health check during start." >&2
    exit 1
  fi

  write_state_file "http://127.0.0.1:${current_port}"
  start_proxy
  echo "Hot-update stack ready. Active slot: $current_slot"
}

deploy_update() {
  local current_slot
  current_slot="$(active_slot)"

  if [[ -z "$current_slot" ]]; then
    bootstrap_stack
    return 0
  fi

  local next_slot old_port next_port
  next_slot="$(inactive_slot)"
  old_port="$(slot_port "$current_slot")"
  next_port="$(slot_port "$next_slot")"

  deploy_slot "$next_slot"
  start_proxy
  write_state_file "http://127.0.0.1:${next_port}"
  write_active_slot "$next_slot"

  echo "Switched proxy to slot $next_slot. Waiting ${HOT_SWITCH_DRAIN_SECONDS}s for in-flight requests."
  sleep "$HOT_SWITCH_DRAIN_SECONDS"

  stop_slot "$current_slot"
  echo "Hot update completed. Active slot: $next_slot (old slot $current_slot on port $old_port stopped)"
}

status() {
  local current_slot
  current_slot="$(active_slot)"
  echo "Active slot: ${current_slot:-none}"
  echo "Blue port:  $HOT_SLOT_BLUE_PORT"
  echo "Green port: $HOT_SLOT_GREEN_PORT"
  if [[ -f "$STATE_FILE" ]]; then
    echo "Proxy target: $(tr -d '\n' < "$STATE_FILE")"
  else
    echo "Proxy target: not configured"
  fi
  if pid_is_running "$PROXY_PID_FILE"; then
    echo "Hot proxy: running"
  else
    echo "Hot proxy: stopped"
  fi
}

stop_all() {
  stop_slot blue || true
  stop_slot green || true
  stop_proxy || true
  rm -f "$ACTIVE_SLOT_FILE" "$STATE_FILE"
  echo "Hot-update stack stopped."
}

ensure_runtime_dirs
ensure_env_file
load_env_file

export JWT_SECRET="${JWT_SECRET:-please-change-me}"
export DEFAULT_ADMIN_PASSWORD="${DEFAULT_ADMIN_PASSWORD:-admin}"
export FIRST_MONDAY="${FIRST_MONDAY:-20260302}"

case "$COMMAND" in
  deploy)
    require_command curl
    require_command go
    resolve_python_bin
    ABS_DATABASE_PATH="$(resolve_path "$BACKEND_DIR" "${DATABASE_PATH:-../data/personnel.db}")"
    ABS_MEMBER_PATH="$(resolve_path "$BACKEND_DIR" "${PRIVATE_MEMBERS_PATH:-../data/member.json}")"
    deploy_update
    ;;
  start)
    require_command curl
    require_command go
    resolve_python_bin
    ABS_DATABASE_PATH="$(resolve_path "$BACKEND_DIR" "${DATABASE_PATH:-../data/personnel.db}")"
    ABS_MEMBER_PATH="$(resolve_path "$BACKEND_DIR" "${PRIVATE_MEMBERS_PATH:-../data/member.json}")"
    ensure_stack_started
    ;;
  status)
    status
    ;;
  stop)
    stop_all
    ;;
  *)
    echo "Usage: $0 [deploy|start|status|stop]" >&2
    exit 1
    ;;
esac
