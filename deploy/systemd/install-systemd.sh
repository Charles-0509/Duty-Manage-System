#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
ROOT_DIR="$(cd -- "$SCRIPT_DIR/../.." && pwd)"

DMS_USER="${DMS_SERVICE_USER:-$(id -un)}"
DMS_GROUP="${DMS_SERVICE_GROUP:-$(id -gn)}"
DMS_DIR="${DMS_INSTALL_DIR:-$ROOT_DIR}"
SYSTEMD_DIR="${SYSTEMD_DIR:-/etc/systemd/system}"

escape_sed_replacement() {
  printf '%s' "$1" | sed -e 's/[\/&]/\\&/g'
}

render_unit() {
  local source_file="$1"
  local target_file="$2"
  local escaped_user escaped_group escaped_dir

  escaped_user="$(escape_sed_replacement "$DMS_USER")"
  escaped_group="$(escape_sed_replacement "$DMS_GROUP")"
  escaped_dir="$(escape_sed_replacement "$DMS_DIR")"

  sed \
    -e "s/__DMS_USER__/$escaped_user/g" \
    -e "s/__DMS_GROUP__/$escaped_group/g" \
    -e "s/__DMS_DIR__/$escaped_dir/g" \
    "$source_file" | sudo tee "$SYSTEMD_DIR/$target_file" >/dev/null
}

render_unit "$SCRIPT_DIR/dms.service" "dms.service"
render_unit "$SCRIPT_DIR/dms-backup.service" "dms-backup.service"
sudo cp "$SCRIPT_DIR/dms-backup.timer" "$SYSTEMD_DIR/dms-backup.timer"
sudo systemctl daemon-reload

echo "Installed systemd units:"
echo "  User:      $DMS_USER"
echo "  Group:     $DMS_GROUP"
echo "  Directory: $DMS_DIR"
echo
echo "Enable services with:"
echo "  sudo systemctl enable --now dms.service"
echo "  sudo systemctl enable --now dms-backup.timer"
