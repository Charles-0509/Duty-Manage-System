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

function Import-DotEnv {
  param (
    [Parameter(Mandatory = $true)]
    [string]$Path
  )

  foreach ($line in Get-Content -LiteralPath $Path) {
    $trimmed = $line.Trim()
    if ([string]::IsNullOrWhiteSpace($trimmed) -or $trimmed.StartsWith("#")) {
      continue
    }

    $parts = $trimmed.Split("=", 2)
    if ($parts.Count -ne 2) {
      continue
    }

    $name = $parts[0].Trim()
    $value = $parts[1].Trim()
    if (($value.StartsWith('"') -and $value.EndsWith('"')) -or ($value.StartsWith("'") -and $value.EndsWith("'"))) {
      $value = $value.Substring(1, $value.Length - 2)
    }

    Set-Item -Path ("Env:{0}" -f $name) -Value $value
  }
}

$root = Split-Path -Parent $MyInvocation.MyCommand.Path
$backendDir = Join-Path $root "backend"
$envFile = Join-Path $backendDir ".env"
$envExampleFile = Join-Path $backendDir ".env.example"
$goExe = "go"
$binaryPath = Join-Path $root "personnel-management.exe"

if (-not (Test-Path $envFile)) {
  if (-not (Test-Path $envExampleFile)) {
    throw ("Missing env template: {0}" -f $envExampleFile)
  }

  Copy-Item -LiteralPath $envExampleFile -Destination $envFile
  Write-Host ("Created {0} from {1}" -f $envFile, $envExampleFile)
  Write-Host ("Please update JWT_SECRET in {0} before production use." -f $envFile) -ForegroundColor Yellow
}

Import-DotEnv -Path $envFile

if (-not (Get-Command go -ErrorAction SilentlyContinue)) {
  $candidate = "C:\SDK\Go\bin\go.exe"
  if (Test-Path $candidate) {
    $goExe = $candidate
  } elseif (-not (Test-Path $binaryPath)) {
    throw "go command not found. Install Go or add go.exe to PATH."
  }
}

$preferredPort = if ($env:APP_PORT) { [int]$env:APP_PORT } else { 3000 }
$env:APP_PORT = (Get-AvailablePort -StartPort $preferredPort).ToString()
$env:DATABASE_PATH = if ($env:DATABASE_PATH) { $env:DATABASE_PATH } else { "..\\data\\personnel.db" }
$env:PRIVATE_MEMBERS_PATH = if ($env:PRIVATE_MEMBERS_PATH) { $env:PRIVATE_MEMBERS_PATH } else { "..\\data\\member.json" }
$env:JWT_SECRET = if ($env:JWT_SECRET) { $env:JWT_SECRET } else { "please-change-me" }
$env:DEFAULT_ADMIN_PASSWORD = if ($env:DEFAULT_ADMIN_PASSWORD) { $env:DEFAULT_ADMIN_PASSWORD } else { "admin" }
$env:FIRST_MONDAY = if ($env:FIRST_MONDAY) { $env:FIRST_MONDAY } else { "20260302" }
$env:GIN_MODE = if ($env:GIN_MODE) { $env:GIN_MODE } else { "release" }

if ($env:JWT_SECRET -eq "please-change-me") {
  Write-Host "Warning: JWT_SECRET is still the default value. Update backend/.env before exposing this system." -ForegroundColor Yellow
}

Write-Host ("Starting DMS on http://127.0.0.1:{0}" -f $env:APP_PORT)
Write-Host ("Database file: {0}" -f $env:DATABASE_PATH)
Write-Host ("Member file: {0}" -f $env:PRIVATE_MEMBERS_PATH)

Push-Location $backendDir
try {
  if (Test-Path $binaryPath) {
    & $binaryPath
  } else {
    & $goExe run ./cmd/server
  }
} finally {
  Pop-Location
}
