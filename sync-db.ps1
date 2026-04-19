$ErrorActionPreference = "Stop"

$root = Split-Path -Parent $MyInvocation.MyCommand.Path
$binaryPath = Join-Path $root "db-sync.exe"
$pushBinaryPath = Join-Path $root "db-push.exe"

if ($env:SYNC_TARGET_URL) {
  if (Test-Path $pushBinaryPath) {
    & $pushBinaryPath
    exit $LASTEXITCODE
  }

  Push-Location (Join-Path $root "backend")
  try {
    go run ./cmd/dbpush
  } finally {
    Pop-Location
  }

  exit $LASTEXITCODE
}

if (Test-Path $binaryPath) {
  & $binaryPath
  exit $LASTEXITCODE
}

if ($env:SYNC_SOURCE_URL) {
  Push-Location (Join-Path $root "backend")
  try {
    go run ./cmd/dbsync
  } finally {
    Pop-Location
  }

  exit $LASTEXITCODE
}

throw "Either SYNC_TARGET_URL or SYNC_SOURCE_URL must be set."
