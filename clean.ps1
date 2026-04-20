[CmdletBinding(SupportsShouldProcess = $true)]
param()

$ErrorActionPreference = "Stop"

$root = Split-Path -Parent $MyInvocation.MyCommand.Path

$filesToRemove = @(
  (Join-Path $root "personnel-management"),
  (Join-Path $root "personnel-management.exe"),
  (Join-Path $root "backend\\server"),
  (Join-Path $root "backend\\server.exe"),
  (Join-Path $root "backend\\dbpush"),
  (Join-Path $root "backend\\dbpush.exe"),
  (Join-Path $root "backend\\dbsync"),
  (Join-Path $root "backend\\dbsync.exe")
)

$dirsToRemove = @(
  (Join-Path $root "frontend\\dist"),
  (Join-Path $root "backend\\internal\\http\\web\\dist")
)

$removedAny = $false

foreach ($target in $filesToRemove) {
  if (Test-Path $target -PathType Leaf) {
    if ($PSCmdlet.ShouldProcess($target, "Remove file")) {
      Remove-Item -LiteralPath $target -Force
      Write-Host "Removed file: $target"
    }
    $removedAny = $true
  }
}

foreach ($target in $dirsToRemove) {
  if (Test-Path $target -PathType Container) {
    if ($PSCmdlet.ShouldProcess($target, "Remove directory")) {
      Remove-Item -LiteralPath $target -Recurse -Force
      Write-Host "Removed directory: $target"
    }
    $removedAny = $true
  }
}

if (-not $removedAny) {
  Write-Host "No local build artifacts found."
} elseif (-not $WhatIfPreference) {
  Write-Host "Local build artifacts cleaned."
}
