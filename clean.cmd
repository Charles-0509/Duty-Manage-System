@echo off
setlocal

set "ROOT=%~dp0"
set "SCRIPT=%ROOT%clean.ps1"

if not exist "%SCRIPT%" (
  echo clean.ps1 not found: "%SCRIPT%"
  exit /b 1
)

where pwsh >nul 2>nul
if %errorlevel%==0 (
  pwsh -NoProfile -ExecutionPolicy Bypass -File "%SCRIPT%" %*
  exit /b %errorlevel%
)

where powershell >nul 2>nul
if %errorlevel%==0 (
  powershell -NoProfile -ExecutionPolicy Bypass -File "%SCRIPT%" %*
  exit /b %errorlevel%
)

echo PowerShell not found. Install Windows PowerShell or PowerShell 7 first.
exit /b 1
