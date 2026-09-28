# Atlas for iPhone

The app icon uses the agreed Folded Path Atlas identity. See [brand/README.md](../brand/README.md) for the vector masters, palette, and export rules.

This is a native SwiftUI iPhone app. It is an Xcode project, not a web app. The app uses the existing durable routing conversation API for typed and push-to-talk input, follow-up questions, a review screen, explicit spoken or typed confirmation, cancellation, and session recovery. Tasks, reminders, and notes are read-only in the app. All changes go through conversation.

## Build and install on an iPhone

1. Install Xcode from the Mac App Store. Launch it once and let it install its required components.
2. In Xcode → Settings → Apple Accounts, sign in with your Apple Account. A free account appears as a **Personal Team**.
3. Open `AtlasPhone.xcodeproj`. Select the AtlasPhone project, then the AtlasPhone target → Signing & Capabilities. Leave **Automatically manage signing** on and select your team. If Xcode reports that the bundle identifier is taken, change `com.atlas.personal` to a unique value.
4. Connect your iPhone to the Mac with a cable, unlock it, and approve any trust prompt. Select the phone as Xcode's run destination. If Xcode requests Developer Mode, on the iPhone open Settings → Privacy & Security → Developer Mode, turn it on, restart, and confirm.
5. Choose Product → Run. Xcode signs, installs, and launches Atlas on the phone.
6. Grant microphone and speech recognition permissions when you first use voice input. Spoken text remains editable until you tap Send. At the review stage, say or type `yes` and send it to save; say or type `cancel` to discard.

A free personal Apple Account can sign a development build for your own device. Apple says personal-team provisioning profiles expire after seven days, so you may need to run the app from Xcode again. No signed `.ipa` can be produced on this Linux machine; the final device build and signing need Xcode on a Mac.

## Connect to Atlas, including away from home

The app accepts a root HTTPS URL in Connection. Atlas currently has no login and binds to `127.0.0.1:8080` by default. Do not forward that port to the public internet. A private Tailscale network with **Tailscale Serve** can provide an HTTPS URL reachable by your iPhone at home or away. Install Tailscale on the Atlas host and the iPhone, sign both into the same tailnet, then on the host run:

```sh
make run
# In another terminal, after enabling HTTPS for the tailnet:
tailscale serve 8080
```

Tailscale prints a private `https://…ts.net` URL. Enter that URL in the app's Connection screen. Keep Atlas on loopback; Tailscale Serve proxies to it. Use Serve, not Funnel. Tailscale is not installed or configured by this repository, so remote access is not live until you complete that setup. Other authenticated private HTTPS reverse proxies can also work.

For a local iOS Simulator on the same Mac as the Atlas server, `http://127.0.0.1:8080` is accepted. The app rejects other plain HTTP addresses and uses normal iOS certificate validation.

## Current limits

- The Atlas server must be running and reachable to send or resume conversations. Draft text is saved locally; API responses are not cached for offline edits.
- Atlas uses Jev for the initial route. It needs the configured OpenRouter key. Follow-up turns reuse the routing result and do not make another Jev call.
- Speech transcription uses Apple's Speech framework. It requests on-device recognition when the current language supports it; otherwise Apple's recognition service may require network access. Typing remains available.
- Reminder delivery remains in Atlas's webpage inbox; this native app has no background push notification channel yet.
- We cannot compile or test this Xcode project on this Linux host. The first device build and microphone test are required before calling this a tested iPhone release.

The existing `/` testing page has a focused **Voice** tab for exercising the same routing conversation flow with mock fixtures. It does not require a Jev call for those fixtures.
