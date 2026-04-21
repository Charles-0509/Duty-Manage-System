#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
TARGET_BRANCH="${UPDATE_BRANCH:-}"

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

require_command git

cd "$ROOT_DIR"

TARGET_BRANCH="$(detect_target_branch)"

echo "Updating repository in $ROOT_DIR"
echo "Target branch: $TARGET_BRANCH"

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
  "$ROOT_DIR/update.sh"

echo "Repository updated to origin/$TARGET_BRANCH"
