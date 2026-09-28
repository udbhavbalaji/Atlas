# Atlas for Omarchy

The Linux desktop app opens a conversation-first Atlas interface in a GTK 3 and WebKitGTK 4.1 window. It starts a private loopback Go server on an available port and stops it when the window closes. Records are stored in `${XDG_DATA_HOME:-~/.local/share}/atlas/atlas.db`; an optional OpenRouter key can be placed in `${XDG_CONFIG_HOME:-~/.config}/atlas/openrouter.key` or supplied via `OPENROUTER_API_KEY`.

This was built against Omarchy 4.0.1 on x86_64 Wayland/Hyprland. GTK 3, WebKitGTK 4.1, their development headers, Go 1.27, and `pkg-config` are required to build. The installed app requires the GTK/WebKit runtime libraries.

Run `desktop/linux/build.sh` and then `dist/linux/atlas-desktop`, or run `desktop/linux/install.sh` to place the app in the user's local application directory and add it to the launcher. No Omarchy configuration changes are needed.

The native window starts on the conversation screen. Use **Records and tests** to open the full browser interface, or **Desktop checks** to verify service, persisted data, and web engine capabilities. WebKitGTK may lack the browser speech recognition API. On Omarchy, click **Focus for dictation**, then hold **F9** or toggle **Super+Ctrl+X** to use Voxtype; review the inserted text before sending. Typed turns use the same conversation. Reminders currently appear in the in-app inbox, not as OS notifications.
