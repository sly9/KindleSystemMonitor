#Requires -Version 5.1
# Installs kindle-dash on Windows: builds (if Go is available), copies the
# binary into a stable location, registers Task Scheduler autostart with
# HIGHEST privileges, prints status.
#
# Requires admin (UAC prompt once): the daemon talks to the PawnIO kernel
# driver to read CPU temperature, which only accepts elevated processes.
# `schtasks /RL HIGHEST` lets the daemon launch as admin at login without
# further UAC prompts.

$ErrorActionPreference = "Stop"

# Self-elevate if we don't already have admin.
$isAdmin = ([Security.Principal.WindowsPrincipal] [Security.Principal.WindowsIdentity]::GetCurrent()).IsInRole([Security.Principal.WindowsBuiltInRole]::Administrator)
if (-not $isAdmin) {
    Write-Host "Re-launching this script elevated (UAC prompt incoming)..." -ForegroundColor Yellow
    $args = @("-NoProfile", "-ExecutionPolicy", "Bypass", "-File", "`"$PSCommandPath`"")
    Start-Process -FilePath powershell.exe -ArgumentList $args -Verb RunAs -Wait
    exit $LASTEXITCODE
}

Write-Host "kindle-dash install (Windows, elevated)" -ForegroundColor Cyan
Write-Host ""

# 1. Locate the go/ source tree (this script lives in <repo>/scripts/).
$repoRoot = (Resolve-Path "$PSScriptRoot\..").Path
$goDir    = Join-Path $repoRoot "go"
if (-not (Test-Path $goDir)) {
    Write-Host "ERROR: cannot find $goDir; run this script from the repo's scripts/ folder." -ForegroundColor Red
    exit 1
}

# 2. Always build with -H windowsgui so Task Scheduler runs silently (no console window).
$srcBin = Join-Path $goDir "kindle-dash.exe"
# PATH refresh: scoop/winget put Go on the user PATH, but the current
# shell may have started before Go was installed.
$env:Path = [Environment]::GetEnvironmentVariable("Path","User") + ";" + [Environment]::GetEnvironmentVariable("Path","Machine")
if (-not (Get-Command go -ErrorAction SilentlyContinue)) {
    Write-Host "ERROR: Go toolchain not found on PATH." -ForegroundColor Red
    Write-Host "  Install Go first:  scoop install go    (or:  winget install GoLang.Go)"
    Write-Host "  Then re-run this script, or pre-build manually:"
    Write-Host "      cd $goDir; go build -ldflags `"-H windowsgui`" -o kindle-dash.exe ./cmd/kindle-dash"
    exit 1
}
Write-Host "Building kindle-dash.exe (-H windowsgui) ..." -ForegroundColor Yellow
Push-Location $goDir
try {
    & go build -ldflags "-H windowsgui" -o kindle-dash.exe ./cmd/kindle-dash
    if ($LASTEXITCODE -ne 0) { throw "go build failed (exit $LASTEXITCODE)" }
} finally { Pop-Location }

# 3. Copy to %LOCALAPPDATA%\Programs\kindle-dash\.
$installDir   = Join-Path $env:LOCALAPPDATA "Programs\kindle-dash"
$installedExe = Join-Path $installDir "kindle-dash.exe"
New-Item -ItemType Directory -Path $installDir -Force | Out-Null

# Stop any running instance so we can overwrite the file.
if (Test-Path $installedExe) {
    & $installedExe stop 2>$null | Out-Null
    Start-Sleep -Milliseconds 500
}
Copy-Item $srcBin $installedExe -Force
Write-Host "Copied binary -> $installedExe"

# 4. Register HKCU\Run autostart.
& $installedExe install

# 5. Show final status.
Write-Host ""
& $installedExe status

Write-Host ""
Write-Host "Done." -ForegroundColor Green
Write-Host "Config file: $env:APPDATA\kindle-dash\config.json"
Write-Host "  (set kindle.host to your Kindle's IP if not already configured)"
Write-Host ""
Write-Host "PawnIO check:" -ForegroundColor Cyan
if (Test-Path "C:\Program Files\PawnIO\PawnIO.sys") {
    Write-Host "  [OK] PawnIO is installed - CPU temperature will work."
} else {
    Write-Host "  [--] PawnIO NOT installed. CPU temperature will read N/A unless you install it:"
    Write-Host "       https://github.com/namazso/PawnIO/releases"
    Write-Host "       (kindle-dash works fine without it; you just won't see CPU TEMP.)"
}
Write-Host ""
Write-Host "Quick commands:"
Write-Host "  $installedExe doctor       # verify SSH + Kindle reachability"
Write-Host "  $installedExe start        # start the autostart instance now"
Write-Host "  $installedExe stop         # stop it"
Write-Host "  $installedExe status       # see installed / running state"
Write-Host "  $installedExe run          # foreground run with logs (Ctrl-C to stop + push farewell)"
Write-Host ""
Write-Host "Autostart triggers on the next user login (logging out and back in is enough)."
