#Requires -Version 5.1
# Removes the kindle-dash autostart registration (and optionally the
# installed binary + config).

$ErrorActionPreference = "Stop"

Write-Host "kindle-dash uninstall (Windows)" -ForegroundColor Cyan
Write-Host ""

$installDir   = Join-Path $env:LOCALAPPDATA "Programs\kindle-dash"
$installedExe = Join-Path $installDir "kindle-dash.exe"

if (Test-Path $installedExe) {
    & $installedExe uninstall
} else {
    Write-Host "kindle-dash.exe not found at $installedExe; cleaning registry + processes directly..."
    Remove-ItemProperty -Path "HKCU:\Software\Microsoft\Windows\CurrentVersion\Run" -Name KindleDash -ErrorAction SilentlyContinue
    Get-Process kindle-dash -ErrorAction SilentlyContinue | Stop-Process -Force
    Write-Host "kindle-dash: autostart removed (or wasn't installed)."
}

Write-Host ""
$ans = 'N'
try { $ans = Read-Host "Also delete installed files (binary + config)? [y/N]" } catch {
    Write-Host "(NonInteractive shell — skipping file deletion. Run again interactively to remove files.)"
}
if ($ans -eq 'y' -or $ans -eq 'Y') {
    Remove-Item -Recurse -Force $installDir -ErrorAction SilentlyContinue
    Remove-Item -Recurse -Force (Join-Path $env:APPDATA "kindle-dash") -ErrorAction SilentlyContinue
    Remove-Item -Recurse -Force (Join-Path $env:LOCALAPPDATA "kindle-dash") -ErrorAction SilentlyContinue
    Write-Host "Deleted binary and config."
}
