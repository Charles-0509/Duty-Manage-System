#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
TARGET_BRANCH="${UPDATE_BRANCH:-}"
SERVICE_NAME="${UPDATE_SERVICE_NAME:-dms.service}"
MANAGE_SERVICE="${UPDATE_MANAGE_SERVICE:-1}"
SERVICE_WAS_ACTIVE=0

require_command() {
  if ! command -v "$1" >/dev/null 2>&1; then
    echo "Missing required command: $1" >&2
    exit 1
  fi
}

detect_target_branch() {
  if [[ -n "$TARGET_BRANCH" ]]; then
    printf '%s\n' "$TARGET_BRANCH"
    return 0
  fi

  local current_branch
  current_branch="$(git -C "$ROOT_DIR" branch --show-current)"
  if [[ -n "$current_branch" ]]; then
    printf '%s\n' "$current_branch"
    return 0
  fi

  printf 'main\n'
}

run_systemctl() {
  if [[ "${EUID:-$(id -u)}" -eq 0 ]]; then
    systemctl "$@"
    return 0
  fi

  if command -v sudo >/dev/null 2>&1; then
    sudo systemctl "$@"
    return 0
  fi

  echo "systemctl requires root privileges, and sudo is not available." >&2
  exit 1
}

service_exists() {
  if ! command -v systemctl >/dev/null 2>&1; then
    return 1
  fi

  if [[ "${EUID:-$(id -u)}" -eq 0 ]]; then
    systemctl list-unit-files "$SERVICE_NAME" --no-legend 2>/dev/null | grep -q .
    return
  fi

  if command -v sudo >/dev/null 2>&1; then
    sudo systemctl list-unit-files "$SERVICE_NAME" --no-legend 2>/dev/null | grep -q .
    return
  fi

  return 1
}

service_is_active() {
  run_systemctl is-active --quiet "$SERVICE_NAME"
}

restore_service_if_needed() {
  local exit_code="$1"

  if [[ "$MANAGE_SERVICE" == "0" || "$SERVICE_WAS_ACTIVE" -ne 1 ]]; then
    return "$exit_code"
  fi

  if [[ "$exit_code" -eq 0 ]]; then
    return 0
  fi

  echo "Update failed. Attempting to restore $SERVICE_NAME..." >&2
  if ! run_systemctl start "$SERVICE_NAME"; then
    echo "Failed to restore $SERVICE_NAME automatically. Please check the service manually." >&2
  fi

  return "$exit_code"
}

require_command git

cd "$ROOT_DIR"

TARGET_BRANCH="$(detect_target_branch)"

echo "Updating repository in $ROOT_DIR"
echo "Target branch: $TARGET_BRANCH"

trap 'restore_service_if_needed "$?"' EXIT

if [[ "$MANAGE_SERVICE" != "0" && "$(service_exists && echo yes || echo no)" == "yes" ]]; then
  if service_is_active; then
    SERVICE_WAS_ACTIVE=1
    echo "Stopping $SERVICE_NAME"
    run_systemctl stop "$SERVICE_NAME"
  else
    echo "$SERVICE_NAME is not active. Skipping stop."
  fi
fi

git fetch origin --prune

if ! git show-ref --verify --quiet "refs/remotes/origin/$TARGET_BRANCH"; then
  echo "Remote branch not found: origin/$TARGET_BRANCH" >&2
  exit 1
fi

git reset --hard "origin/$TARGET_BRANCH"
git clean -fd

chmod +x \
  "$ROOT_DIR/build.sh" \
  "$ROOT_DIR/run.sh" \
  "$ROOT_DIR/clean.sh" \
  "$ROOT_DIR/update.sh" \
  "$ROOT_DIR/backup.sh"

echo "Repository updated to origin/$TARGET_BRANCH"
echo "Building project"
"$ROOT_DIR/build.sh"

if [[ "$MANAGE_SERVICE" != "0" && "$SERVICE_WAS_ACTIVE" -eq 1 ]]; then
  echo "Starting $SERVICE_NAME"
  run_systemctl start "$SERVICE_NAME"
  echo "$SERVICE_NAME restarted successfully"
fi
