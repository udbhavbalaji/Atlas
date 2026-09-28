# Atlas for macOS

The macOS preview is a native AppKit window with WKWebView. It opens the same conversation-first interface as the Omarchy app and runs the Go Atlas service only while the window is open. Records and the optional `openrouter.key` live in `~/Library/Application Support/Atlas/`.

On a Mac with Xcode command line tools and Go 1.27 or newer, run `desktop/macos/build.sh`, then `open dist/macos/Atlas.app`. The build script signs the local bundle with an ad hoc signature. It is not notarized or packaged for distribution.

Open **Desktop checks** from the conversation screen to test service, storage, and speech capabilities. Use **Records and tests** for the full task, reminder, note, and API interfaces. Speech recognition availability depends on the installed macOS WebKit version; typed turns use the same durable conversation path. Reminders currently surface in the in-app inbox, not as macOS notifications.

The Go server can be cross-compiled on Linux, but this native shell must be compiled and run on a Mac. macOS window behavior and microphone permission have not yet been verified on a Mac.
