# Atlas for Omarchy

The Linux desktop app opens a conversation-first Atlas interface in a GTK 3 and WebKitGTK 4.1 window. It starts a private loopback Go server on an available port and stops it when the window closes. Records are stored in `${XDG_DATA_HOME:-~/.local/share}/atlas/atlas.db`; an optional OpenRouter key can be placed in `${XDG_CONFIG_HOME:-~/.config}/atlas/openrouter.key` or supplied via `OPENROUTER_API_KEY`.

This was built against Omarchy 4.0.1 on x86_64 Wayland/Hyprland. GTK 3, WebKitGTK 4.1, their development headers, Go 1.27, and `pkg-config` are required to build. The installed app requires the GTK/WebKit runtime libraries.

Run `desktop/linux/build.sh` and then `dist/linux/atlas-desktop`, or run `desktop/linux/install.sh` to place the app in the user's local application directory and add it to the launcher. No Omarchy configuration changes are needed.

The native window starts on the conversation screen. Use **Records and tests** to open the full browser interface, or **Desktop checks** to verify service, persisted data, and audio capabilities. Click **Start microphone** to stream PCM audio to the local Atlas service. It uses the installed Voxtype model for provisional transcripts around five and ten seconds, then a final transcript when you stop (30 second limit). Review or edit the text before sending. Atlas discards the audio after transcription, including its temporary WAV file. If Voxtype is unavailable, install and configure it first; the page also supports typed input. Reminders currently appear in the in-app inbox, not as OS notifications.
