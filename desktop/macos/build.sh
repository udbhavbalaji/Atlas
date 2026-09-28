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
echo "Built $app"
