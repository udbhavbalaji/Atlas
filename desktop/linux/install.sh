#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
install_dir="${XDG_DATA_HOME:-$HOME/.local/share}/atlas/app"
application_dir="${XDG_DATA_HOME:-$HOME/.local/share}/applications"
"$repo_root/desktop/linux/build.sh" "$repo_root/dist/linux"
install -d -m 700 "$install_dir"
install -m 755 "$repo_root/dist/linux/atlas-server" "$install_dir/atlas-server"
install -m 755 "$repo_root/dist/linux/atlas-desktop" "$install_dir/atlas-desktop"
install -d "$application_dir"
desktop_file="$application_dir/atlas.desktop"
cat > "$desktop_file" <<EOF
[Desktop Entry]
Type=Application
Name=Atlas
Comment=Local tasks, notes, and reminders
Exec=$install_dir/atlas-desktop
Terminal=false
Categories=Office;Utility;
StartupNotify=true
EOF
chmod 644 "$desktop_file"
echo "Installed Atlas desktop at $install_dir and $desktop_file"
