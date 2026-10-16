#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
install_dir="${XDG_DATA_HOME:-$HOME/.local/share}/atlas/app"
application_dir="${XDG_DATA_HOME:-$HOME/.local/share}/applications"
icon_dir="${XDG_DATA_HOME:-$HOME/.local/share}/icons/hicolor/scalable/apps"
config_dir="${XDG_CONFIG_HOME:-$HOME/.config}/atlas"
"$repo_root/desktop/linux/build.sh" "$repo_root/dist/linux"
if ! command -v gst-inspect-1.0 >/dev/null || ! gst-inspect-1.0 autoaudiosink >/dev/null 2>&1; then
  echo "Microphone capture needs gst-plugins-good. Install it with: omarchy pkg add gst-plugins-good" >&2
fi
install -d -m 700 "$install_dir"
install -m 755 "$repo_root/dist/linux/atlas-server" "$install_dir/atlas-server"
install -m 755 "$repo_root/dist/linux/atlas-desktop" "$install_dir/atlas-desktop"
install -d "$application_dir"
install -d "$icon_dir"
install -m 644 "$repo_root/desktop/linux/atlas.svg" "$icon_dir/atlas.svg"
if [[ -s "$repo_root/data/openrouter.key" && ! -e "$config_dir/openrouter.key" ]]; then
  install -d -m 700 "$config_dir"
  install -m 600 "$repo_root/data/openrouter.key" "$config_dir/openrouter.key"
  echo "Configured the existing OpenRouter key for the desktop app."
fi
if [[ -s "$repo_root/data/groq.key" && ! -e "$config_dir/groq.key" ]]; then
  install -d -m 700 "$config_dir"
  install -m 600 "$repo_root/data/groq.key" "$config_dir/groq.key"
  echo "Configured the existing Groq key for the desktop app."
fi
if command -v python3 >/dev/null && ! pgrep -u "$(id -u)" -x atlas-desktop >/dev/null; then
  python3 "$repo_root/desktop/linux/import-existing-data.py" "$repo_root/data/atlas.db" "${XDG_DATA_HOME:-$HOME/.local/share}/atlas/atlas.db"
else
  echo "Close Atlas and rerun install.sh to import existing records into an empty desktop database." >&2
fi
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
