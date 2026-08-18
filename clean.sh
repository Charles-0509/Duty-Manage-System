#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"

rm -f "$ROOT_DIR/personnel-management" "$ROOT_DIR/dms"
rm -rf "$ROOT_DIR/frontend/dist" "$ROOT_DIR/backend/internal/http/web/dist"
echo "Local build artifacts cleaned."
