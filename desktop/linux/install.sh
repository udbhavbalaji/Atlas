#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
install_dir="${XDG_DATA_HOME:-$HOME/.local/share}/atlas/app"
application_dir="${XDG_DATA_HOME:-$HOME/.local/share}/applications"
icon_dir="${XDG_DATA_HOME:-$HOME/.local/share}/icons/hicolor/scalable/apps"
"$repo_root/desktop/linux/build.sh" "$repo_root/dist/linux"
install -d -m 700 "$install_dir"
install -m 755 "$repo_root/dist/linux/atlas-server" "$install_dir/atlas-server"
install -m 755 "$repo_root/dist/linux/atlas-desktop" "$install_dir/atlas-desktop"
install -d "$application_dir"
install -d "$icon_dir"
install -m 644 "$repo_root/desktop/linux/atlas.svg" "$icon_dir/atlas.svg"
desktop_file="$application_dir/atlas.desktop"
cat > "$desktop_file" <<EOF
[Desktop Entry]
Type=Application
Name=Atlas
Comment=Local tasks, notes, and reminders
Exec=$install_dir/atlas-desktop
Icon=atlas
Terminal=false
Categories=Office;
StartupNotify=true
StartupWMClass=atlas-desktop
EOF
chmod 644 "$desktop_file"
echo "Installed Atlas desktop at $install_dir and $desktop_file"
