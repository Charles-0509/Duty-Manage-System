$ErrorActionPreference = "Stop"

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

if (-not (Test-Path $envFile)) {
  if (-not (Test-Path $envExampleFile)) {
    throw ("Missing env template: {0}" -f $envExampleFile)
  }

  Copy-Item -LiteralPath $envExampleFile -Destination $envFile
  Write-Host ("Created {0} from {1}" -f $envFile, $envExampleFile)
  Write-Host ("Please update JWT_SECRET in {0} before production use." -f $envFile) -ForegroundColor Yellow
}

Import-DotEnv -Path $envFile

if ($env:JWT_SECRET -eq "please-change-me") {
  Write-Host "Warning: JWT_SECRET is still the default value. Update backend/.env before production use." -ForegroundColor Yellow
}

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
    if (Test-Path "package-lock.json") {
      npm ci --no-audit --no-fund
    } else {
      npm install --no-audit --no-fund
    }
  }
  npm run build
} finally {
  Pop-Location
}

$embedDist = Join-Path $root "backend\internal\http\web\dist"
if (Test-Path $embedDist) {
  Remove-Item -LiteralPath $embedDist -Recurse -Force
}
Copy-Item -LiteralPath (Join-Path $root "frontend\dist") -Destination $embedDist -Recurse -Force

Push-Location $backendDir
try {
  & $goExe build -o (Join-Path $root "personnel-management.exe") ./cmd/server
} finally {
  Pop-Location
}

Write-Host "Build completed:" (Join-Path $root "personnel-management.exe")
