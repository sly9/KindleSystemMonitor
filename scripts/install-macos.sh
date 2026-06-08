#!/usr/bin/env bash
# Installs kindle-dash on macOS: builds (if Go is available), copies the
# binary into ~/.local/bin, registers a launchd LaunchAgent, prints status.
# User-level (LaunchAgent under $HOME/Library), no sudo required.

set -euo pipefail

echo "kindle-dash install (macOS)"
echo

# 1. Locate the go/ source tree (this script lives in <repo>/scripts/).
repo_root=$(cd "$(dirname "$0")/.." && pwd)
go_dir="$repo_root/go"
if [ ! -d "$go_dir" ]; then
    echo "ERROR: cannot find $go_dir; run this script from the repo's scripts/ folder." >&2
    exit 1
fi

# 2. Build if the binary isn't already present.
src_bin="$go_dir/kindle-dash"
if [ ! -x "$src_bin" ]; then
    if ! command -v go >/dev/null 2>&1; then
        echo "ERROR: Go toolchain not found on PATH." >&2
        echo "  Install Go first:  brew install go" >&2
        echo "  Then re-run this script, or pre-build manually:" >&2
        echo "      cd $go_dir && go build -o kindle-dash ./cmd/kindle-dash" >&2
        exit 1
    fi
    echo "Building kindle-dash ..."
    (cd "$go_dir" && go build -o kindle-dash ./cmd/kindle-dash)
fi

# 3. Copy to ~/.local/bin/.
install_dir="$HOME/.local/bin"
mkdir -p "$install_dir"
installed="$install_dir/kindle-dash"

# Stop any running instance so we can overwrite cleanly.
if [ -x "$installed" ]; then
    "$installed" stop >/dev/null 2>&1 || true
    sleep 0.3
fi
cp "$src_bin" "$installed"
chmod +x "$installed"
echo "Copied binary -> $installed"

# 4. Register launchd LaunchAgent.
"$installed" install

# 5. Show final status.
echo
"$installed" status

cat <<EOF

Done.
Config file: \$HOME/.config/kindle-dash/config.json
  (set kindle.host to your Kindle's IP if not already configured)

Quick commands:
  $installed doctor       # verify SSH + Kindle reachability
  $installed start        # start the autostart instance now
  $installed stop         # stop it
  $installed status       # see installed / running state
  $installed run          # foreground run with logs (Ctrl-C to stop + push farewell)

Logs from the autostart instance:
  ~/.cache/kindle-dash/stdout.log
  ~/.cache/kindle-dash/stderr.log

Autostart triggers on the next user login.
EOF

if ! echo ":$PATH:" | grep -q ":$install_dir:"; then
    echo
    echo "NOTE: $install_dir is not in your PATH. Add it to your shell rc, e.g.:"
    echo "      echo 'export PATH=\"\$HOME/.local/bin:\$PATH\"' >> ~/.zshrc"
fi
