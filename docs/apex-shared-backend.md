# One Atlas backend on apex

Run one Atlas server and one persistent SQLite database on apex. The website, iPhone app, Mac app, and Omarchy app should all use the same private Tailscale Serve HTTPS origin. PR #11 adds the desktop shared-server settings, iPhone app, report flow, records revision polling, and focused `/sync-test` page. Deploy an Atlas release containing both PRs before connecting devices.

## Install and check apex

On apex, from a verified release checkout:

```sh
bash deploy/apex/setup.sh install
bash deploy/apex/setup.sh serve
bash deploy/apex/setup.sh check
```

`install` builds the server and enables a user systemd service bound to `127.0.0.1:8080`. Its canonical database is `~/.local/share/atlas/atlas.db`. It does not copy or combine databases from other devices. Optional `openrouter.key` and `groq.key` files can be placed in `~/.config/atlas/` with mode `0600`. The user service must keep running when you log out; an administrator can enable systemd linger for the Atlas account when needed.

`serve` checks the existing Tailscale Serve configuration first. It keeps a matching private HTTPS root proxy to `http://127.0.0.1:8080`; it creates that mapping only when no mapping exists. It refuses an unrelated mapping or Funnel exposure. The command prints the actual HTTPS URL; do not guess the tailnet DNS suffix. `check` requires a running service and confirms both local and HTTPS health plus task API access. It makes no saved-data changes.

Keep apex and each client in the same tailnet. Review the tailnet's grants or ACLs so only intended identities can reach apex on HTTPS port 443. The default tailnet policy may allow broader access than you want. Atlas has no application-level authentication, and conversation reports include transcripts. Keep the service bound to loopback and use Serve, not Funnel. Back up `atlas.db` before upgrades, then verify the service after each release.

## Connect every interface

Use the HTTPS URL printed by `bash deploy/apex/setup.sh serve` for each interface:

| Interface | Address setting |
| --- | --- |
| Website | Open the apex HTTPS URL in a browser on a tailnet-connected device. |
| iPhone | Enter that root URL in **Connection → Atlas server**. |
| Mac app | Put the root URL in `~/Library/Application Support/Atlas/server-url`, or set `ATLAS_SERVER_URL` when launching. |
| Omarchy app | Put the root URL in `${XDG_CONFIG_HOME:-~/.config}/atlas/server-url`, or set `ATLAS_SERVER_URL` when launching. |

Open `/sync-test` on each device to confirm the origin and record counts. Create a task on the phone, then confirm the open web or desktop saved-items view shows it without manual refresh; repeat in the opposite direction. Active views check for a new records revision every second. A background iPhone app refreshes when brought to the foreground. Also send a fixture report through `/report-test` and confirm it appears in the apex report list. These are focused browser checks; run a native iPhone and Mac check on devices before rollout.

Changing an app's server address leaves its old local database behind. Export or migrate existing data deliberately before retiring a local profile. This is online shared storage, not offline replication. Clients cannot save while apex is unreachable. The desktop microphone stream is local-only in shared-server mode; typing or system dictation works, and iPhone speech stays device-side.

## Troubleshooting

`systemctl --user status atlas.service` and `journalctl --user -u atlas.service` show service failures. Check `curl -f http://127.0.0.1:8080/healthz` before diagnosing Serve. `tailscale serve status` shows the private proxy. An HTTPS error on one device may mean that device has not joined the tailnet or lacks policy access. The setup script does not edit tailnet policy or enroll devices.
