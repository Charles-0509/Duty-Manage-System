$ErrorActionPreference = "Stop"

$root = Split-Path -Parent $MyInvocation.MyCommand.Path
$goExe = "go"

if (-not (Get-Command go -ErrorAction SilentlyContinue)) {
  $candidate = "C:\SDK\Go\bin\go.exe"
  if (Test-Path $candidate) {
    $goExe = $candidate
  } else {
    throw "go command not found. Install Go or add go.exe to PATH."
  }
}

Push-Location (Join-Path $root "frontend")
try {
  if (-not (Test-Path "node_modules")) {
    npm install
  }
  npm run build
} finally {
  Pop-Location
}

$embedDist = Join-Path $root "backend\internal\http\web\dist"
if (Test-Path $embedDist) {
  Remove-Item -Recurse -Force $embedDist
}
Copy-Item -Recurse -Force (Join-Path $root "frontend\dist") $embedDist

Push-Location (Join-Path $root "backend")
try {
  & $goExe mod tidy
  & $goExe build -o (Join-Path $root "personnel-management.exe") ./cmd/server
} finally {
  Pop-Location
}

Write-Host "Build completed:" (Join-Path $root "personnel-management.exe")
