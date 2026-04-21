#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"

FILES_TO_REMOVE=(
  "$ROOT_DIR/personnel-management"
  "$ROOT_DIR/personnel-management.exe"
  "$ROOT_DIR/backend/server"
  "$ROOT_DIR/backend/server.exe"
  "$ROOT_DIR/backend/dbpush"
  "$ROOT_DIR/backend/dbpush.exe"
  "$ROOT_DIR/backend/dbsync"
  "$ROOT_DIR/backend/dbsync.exe"
)

DIRS_TO_REMOVE=(
  "$ROOT_DIR/frontend/dist"
  "$ROOT_DIR/backend/internal/http/web/dist"
  "$ROOT_DIR/.hot-runtime"
)

removed_any=0

for target in "${FILES_TO_REMOVE[@]}"; do
  if [[ -e "$target" ]]; then
    rm -f "$target"
    echo "Removed file: $target"
    removed_any=1
  fi
done

for target in "${DIRS_TO_REMOVE[@]}"; do
  if [[ -e "$target" ]]; then
    rm -rf "$target"
    echo "Removed directory: $target"
    removed_any=1
  fi
done

if [[ "$removed_any" -eq 0 ]]; then
  echo "No local build artifacts found."
else
  echo "Local build artifacts cleaned."
fi
