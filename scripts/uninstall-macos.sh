#!/usr/bin/env bash
# Removes the kindle-dash LaunchAgent (and optionally binary + config).

set -euo pipefail

echo "kindle-dash uninstall (macOS)"
echo

installed="$HOME/.local/bin/kindle-dash"
plist="$HOME/Library/LaunchAgents/com.kindledash.dash.plist"

if [ -x "$installed" ]; then
    "$installed" uninstall
else
    echo "kindle-dash not found at $installed; cleaning launchd directly..."
    launchctl unload "$plist" 2>/dev/null || true
    rm -f "$plist"
    pkill -f "kindle-dash run" 2>/dev/null || true
    echo "kindle-dash: autostart removed (or wasn't installed)."
fi

echo
read -rp "Also delete installed files (binary + config)? [y/N] " ans
if [[ "${ans:-N}" =~ ^[Yy]$ ]]; then
    rm -f "$installed"
    rm -rf "$HOME/.config/kindle-dash"
    rm -rf "$HOME/.cache/kindle-dash"
    echo "Deleted binary and config."
fi
