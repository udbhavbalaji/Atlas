#!/usr/bin/env bash
set -euo pipefail

if [[ "$(uname -s)" != Darwin ]]; then
  echo "Build Atlas.app on a Mac with Xcode command line tools and Go 1.27 or newer." >&2
  exit 1
fi
repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
output_dir="${1:-$repo_root/dist/macos}"
app="$output_dir/Atlas.app"
mkdir -p "$app/Contents/MacOS" "$app/Contents/Resources"
cp "$repo_root/desktop/macos/Info.plist" "$app/Contents/Info.plist"
swiftc -O -framework AppKit -framework WebKit \
  "$repo_root/desktop/macos/AtlasDesktop.swift" -o "$app/Contents/MacOS/Atlas"
cd "$repo_root"
CGO_ENABLED=0 go build -trimpath -o "$app/Contents/Resources/atlas-server" ./cmd/atlas
codesign --force --sign - "$app/Contents/Resources/atlas-server"
codesign --force --sign - "$app"
profile_dir="$HOME/Library/Application Support/Atlas"
install -d -m 700 "$profile_dir"
if [[ -s "$repo_root/data/openrouter.key" && ! -e "$profile_dir/openrouter.key" ]]; then
  install -m 600 "$repo_root/data/openrouter.key" "$profile_dir/openrouter.key"
  echo "Configured the existing OpenRouter key for the macOS app."
fi
if command -v python3 >/dev/null && ! pgrep -x Atlas >/dev/null; then
  python3 "$repo_root/desktop/macos/import-existing-data.py" "$repo_root/data/atlas.db" "$profile_dir/atlas.db"
else
  echo "Close Atlas and rerun build.sh to import existing records into an empty macOS database." >&2
fi
echo "Built $app"
