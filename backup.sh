#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
BACKEND_DIR="$ROOT_DIR/backend"
ENV_FILE="$BACKEND_DIR/.env"
DEFAULT_DATABASE_PATH="../data/personnel.db"
DEFAULT_MEMBER_PATH="../data/member.json"
DEFAULT_BACKUP_DIR="/home/Charles/DMS-backup"

require_command() {
  if command -v "$1" >/dev/null 2>&1; then
    return 0
  fi

  echo "Missing required command: $1" >&2
  exit 1
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

resolve_path() {
  local base_dir="$1"
  local raw_path="$2"

  python3 - "$base_dir" "$raw_path" <<'PY'
import os
import sys

base_dir, raw_path = sys.argv[1], sys.argv[2]
if os.path.isabs(raw_path):
    print(os.path.abspath(raw_path))
else:
    print(os.path.abspath(os.path.join(base_dir, raw_path)))
PY
}

backup_sqlite() {
  local source_db="$1"
  local target_db="$2"

  python3 - "$source_db" "$target_db" <<'PY'
import os
import sqlite3
import sys

source_db, target_db = sys.argv[1], sys.argv[2]
os.makedirs(os.path.dirname(target_db), exist_ok=True)

source = sqlite3.connect(f"file:{source_db}?mode=ro", uri=True)
target = sqlite3.connect(target_db)

with target:
    source.backup(target)

source.close()
target.close()
PY
}

require_source_file() {
  local label="$1"
  local path="$2"

  if [[ -f "$path" ]]; then
    return 0
  fi

  echo "$label not found: $path" >&2
  exit 1
}

load_env_file
require_command python3

DATABASE_PATH_VALUE="${DATABASE_PATH:-$DEFAULT_DATABASE_PATH}"
PRIVATE_MEMBERS_PATH_VALUE="${PRIVATE_MEMBERS_PATH:-$DEFAULT_MEMBER_PATH}"
BACKUP_DIR="${BACKUP_DIR:-$DEFAULT_BACKUP_DIR}"

ABS_DATABASE_PATH="$(resolve_path "$BACKEND_DIR" "$DATABASE_PATH_VALUE")"
ABS_MEMBER_PATH="$(resolve_path "$BACKEND_DIR" "$PRIVATE_MEMBERS_PATH_VALUE")"

require_source_file "Database file" "$ABS_DATABASE_PATH"
require_source_file "Member file" "$ABS_MEMBER_PATH"

mkdir -p "$BACKUP_DIR"

TIMESTAMP="$(date '+%Y-%m-%d_%H-%M-%S')"
SNAPSHOT_DIR="$BACKUP_DIR/$TIMESTAMP"
LATEST_DIR="$BACKUP_DIR/latest"

mkdir -p "$SNAPSHOT_DIR"
mkdir -p "$LATEST_DIR"

backup_sqlite "$ABS_DATABASE_PATH" "$SNAPSHOT_DIR/personnel.db"
cp "$ABS_MEMBER_PATH" "$SNAPSHOT_DIR/member.json"

backup_sqlite "$ABS_DATABASE_PATH" "$LATEST_DIR/personnel.db"
cp "$ABS_MEMBER_PATH" "$LATEST_DIR/member.json"

echo "Backup completed:"
echo "  Snapshot: $SNAPSHOT_DIR"
echo "  Latest:   $LATEST_DIR"
