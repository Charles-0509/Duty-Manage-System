#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
BACKEND_DIR="$ROOT_DIR/backend"
ENV_FILE="$BACKEND_DIR/.env"
DEFAULT_CONTROL_DATABASE_PATH="../data/control.db"
DEFAULT_SEMESTER_DATABASE_DIR="../data/semesters"
DEFAULT_TEMPLATE_DIR="../data/work-study/templates"
DEFAULT_BACKUP_DIR="${HOME:-/tmp}/DMS-backup"
DEFAULT_BACKUP_GIT_REPO="git@github.com:Charles-0509/DMS-backup.git"
DEFAULT_BACKUP_GIT_BRANCH="main"
DEFAULT_BACKUP_SSH_KEY="${HOME:-}/.ssh/id_ed25519"

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

require_source_dir() {
  local label="$1"
  local path="$2"

  if [[ -d "$path" ]]; then
    return 0
  fi

  echo "$label not found: $path" >&2
  exit 1
}

is_truthy() {
  case "${1:-}" in
    1|true|TRUE|yes|YES|on|ON)
      return 0
      ;;
    *)
      return 1
      ;;
  esac
}

expand_home_path() {
  local raw_path="$1"

  case "$raw_path" in
    "~")
      printf '%s\n' "${HOME:-}"
      ;;
    "~/"*)
      printf '%s/%s\n' "${HOME:-}" "${raw_path#~/}"
      ;;
    *)
      printf '%s\n' "$raw_path"
      ;;
  esac
}

sync_backup_to_git() {
  local repo branch ssh_key author_name author_email

  if ! is_truthy "${BACKUP_GIT_ENABLED:-1}"; then
    echo "Backup Git sync disabled."
    return 0
  fi

  require_command git
  require_command ssh

  repo="${BACKUP_GIT_REPO:-$DEFAULT_BACKUP_GIT_REPO}"
  branch="${BACKUP_GIT_BRANCH:-$DEFAULT_BACKUP_GIT_BRANCH}"
  ssh_key="$(expand_home_path "${BACKUP_SSH_KEY:-$DEFAULT_BACKUP_SSH_KEY}")"
  author_name="${BACKUP_GIT_AUTHOR_NAME:-DMS Backup}"
  author_email="${BACKUP_GIT_AUTHOR_EMAIL:-dms-backup@localhost}"

  if [[ -z "$repo" ]]; then
    echo "BACKUP_GIT_REPO is empty." >&2
    exit 1
  fi

  if [[ -z "$branch" ]]; then
    echo "BACKUP_GIT_BRANCH is empty." >&2
    exit 1
  fi

  if [[ ! -f "$ssh_key" ]]; then
    echo "Backup SSH key not found: $ssh_key" >&2
    echo "Set BACKUP_SSH_KEY in backend/.env or create the default key." >&2
    exit 1
  fi

  if [[ ! -d "$BACKUP_DIR/.git" ]]; then
    git -C "$BACKUP_DIR" init -b "$branch"
  fi

  if git -C "$BACKUP_DIR" remote get-url origin >/dev/null 2>&1; then
    git -C "$BACKUP_DIR" remote set-url origin "$repo"
  else
    git -C "$BACKUP_DIR" remote add origin "$repo"
  fi

  git -C "$BACKUP_DIR" config user.name "$author_name"
  git -C "$BACKUP_DIR" config user.email "$author_email"

  export GIT_SSH_COMMAND="ssh -i $ssh_key -o IdentitiesOnly=yes -o StrictHostKeyChecking=accept-new"

  git -C "$BACKUP_DIR" add .

  if git -C "$BACKUP_DIR" diff --cached --quiet; then
    echo "No backup changes to commit."
    return 0
  fi

  git -C "$BACKUP_DIR" commit -m "DMS backup $TIMESTAMP"

  if git -C "$BACKUP_DIR" ls-remote --exit-code --heads origin "$branch" >/dev/null 2>&1; then
    git -C "$BACKUP_DIR" pull --rebase origin "$branch"
  fi

  git -C "$BACKUP_DIR" push -u origin "$branch"

  echo "Backup pushed:"
  echo "  Repository: $repo"
  echo "  Branch:     $branch"
}

load_env_file
require_command python3

CONTROL_DATABASE_PATH_VALUE="${CONTROL_DATABASE_PATH:-$DEFAULT_CONTROL_DATABASE_PATH}"
SEMESTER_DATABASE_DIR_VALUE="${SEMESTER_DATABASE_DIR:-$DEFAULT_SEMESTER_DATABASE_DIR}"
TEMPLATE_DIR_VALUE="${WORK_STUDY_TEMPLATE_DIR:-$DEFAULT_TEMPLATE_DIR}"
BACKUP_DIR="${BACKUP_DIR:-$DEFAULT_BACKUP_DIR}"

ABS_CONTROL_DATABASE_PATH="$(resolve_path "$BACKEND_DIR" "$CONTROL_DATABASE_PATH_VALUE")"
ABS_SEMESTER_DATABASE_DIR="$(resolve_path "$BACKEND_DIR" "$SEMESTER_DATABASE_DIR_VALUE")"
ABS_TEMPLATE_DIR="$(resolve_path "$BACKEND_DIR" "$TEMPLATE_DIR_VALUE")"

require_source_file "Control database" "$ABS_CONTROL_DATABASE_PATH"
require_source_dir "Semester database directory" "$ABS_SEMESTER_DATABASE_DIR"
require_source_dir "Global template directory" "$ABS_TEMPLATE_DIR"

mkdir -p "$BACKUP_DIR"

TIMESTAMP="$(date '+%Y-%m-%d_%H-%M-%S')"
SNAPSHOT_DIR="$BACKUP_DIR/$TIMESTAMP"
LATEST_DIR="$BACKUP_DIR/latest"

mkdir -p "$SNAPSHOT_DIR"
mkdir -p "$LATEST_DIR"
mkdir -p "$SNAPSHOT_DIR/semesters"
mkdir -p "$SNAPSHOT_DIR/work-study/templates"
rm -rf -- "$LATEST_DIR/semesters" "$LATEST_DIR/work-study/templates"
rm -f -- "$LATEST_DIR/personnel.db" "$LATEST_DIR/member.json"
mkdir -p "$LATEST_DIR/semesters"
mkdir -p "$LATEST_DIR/work-study/templates"

backup_sqlite "$ABS_CONTROL_DATABASE_PATH" "$SNAPSHOT_DIR/control.db"
backup_sqlite "$ABS_CONTROL_DATABASE_PATH" "$LATEST_DIR/control.db"

semester_count=0
while IFS= read -r semester_db; do
  filename="$(basename "$semester_db")"
  backup_sqlite "$semester_db" "$SNAPSHOT_DIR/semesters/$filename"
  backup_sqlite "$semester_db" "$LATEST_DIR/semesters/$filename"
  semester_count=$((semester_count + 1))
done < <(find "$ABS_SEMESTER_DATABASE_DIR" -maxdepth 1 -type f -name '*.db' -print | sort)

if [[ "$semester_count" -eq 0 ]]; then
  echo "No semester databases found in $ABS_SEMESTER_DATABASE_DIR" >&2
  exit 1
fi

cp -R "$ABS_TEMPLATE_DIR/." "$SNAPSHOT_DIR/work-study/templates/"
cp -R "$ABS_TEMPLATE_DIR/." "$LATEST_DIR/work-study/templates/"

echo "Backup completed:"
echo "  Snapshot: $SNAPSHOT_DIR"
echo "  Latest:   $LATEST_DIR"
echo "  Semesters: $semester_count"

sync_backup_to_git
