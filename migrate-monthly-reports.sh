#!/usr/bin/env bash
set -euo pipefail
DMS_MIGRATION_ROOT="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
cd "$DMS_MIGRATION_ROOT"
if [[ ! -x ./dms ]]; then
  echo '请先运行 ./build.sh 构建当前版本，再执行迁移。' >&2
  exit 1
fi
exec ./dms migrate-monthly-reports
