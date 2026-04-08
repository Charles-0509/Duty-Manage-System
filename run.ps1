$ErrorActionPreference = "Stop"

function Test-PortInUse {
  param (
    [int]$Port
  )

  $listeners = Get-NetTCPConnection -State Listen -ErrorAction SilentlyContinue |
    Where-Object { $_.LocalPort -eq $Port }

  return [bool]$listeners
}

function Get-AvailablePort {
  param (
    [int]$StartPort
  )

  for ($port = $StartPort; $port -lt ($StartPort + 50); $port++) {
    if (-not (Test-PortInUse -Port $port)) {
      return $port
    }
  }

  throw ("No available port found from {0} to {1}." -f $StartPort, ($StartPort + 49))
}

$root = Split-Path -Parent $MyInvocation.MyCommand.Path
$goExe = "go"
$binaryPath = Join-Path $root "personnel-management.exe"

if (-not (Get-Command go -ErrorAction SilentlyContinue)) {
  $candidate = "C:\SDK\Go\bin\go.exe"
  if (Test-Path $candidate) {
    $goExe = $candidate
  } elseif (-not (Test-Path $binaryPath)) {
    throw "go command not found. Install Go or add go.exe to PATH."
  }
}

$preferredPort = if ($env:APP_PORT) { [int]$env:APP_PORT } else { 8080 }
$env:APP_PORT = (Get-AvailablePort -StartPort $preferredPort).ToString()
$env:DATABASE_PATH = if ($env:DATABASE_PATH) { $env:DATABASE_PATH } else { (Join-Path $root "data\personnel.db") }
$env:JWT_SECRET = if ($env:JWT_SECRET) { $env:JWT_SECRET } else { "please-change-me" }
$env:DEFAULT_ADMIN_PASSWORD = if ($env:DEFAULT_ADMIN_PASSWORD) { $env:DEFAULT_ADMIN_PASSWORD } else { "admin" }
$env:FIRST_MONDAY = if ($env:FIRST_MONDAY) { $env:FIRST_MONDAY } else { "20260302" }
$env:GIN_MODE = if ($env:GIN_MODE) { $env:GIN_MODE } else { "release" }

Write-Host ("Starting PMS Go Version on http://127.0.0.1:{0}" -f $env:APP_PORT)
Write-Host ("Database file: {0}" -f $env:DATABASE_PATH)

Push-Location (Join-Path $root "backend")
try {
  if (Test-Path $binaryPath) {
    & $binaryPath
  } else {
    & $goExe run ./cmd/server
  }
} finally {
  Pop-Location
}
