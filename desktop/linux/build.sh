#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
output_dir="${1:-$repo_root/dist/linux}"
pkg-config --exists gtk+-3.0 webkit2gtk-4.1 || {
  echo "Atlas desktop needs GTK 3 and WebKitGTK 4.1 development packages." >&2
  exit 1
}
mkdir -p "$output_dir"
cd "$repo_root"
go build -o "$output_dir/atlas-server" ./cmd/atlas
CGO_ENABLED=1 go build -o "$output_dir/atlas-desktop" ./cmd/atlas-desktop
echo "Built $output_dir/atlas-desktop"
