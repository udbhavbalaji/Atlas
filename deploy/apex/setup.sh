#!/usr/bin/env bash
set -euo pipefail

repo_root=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/../.." && pwd)
service_file="$repo_root/deploy/apex/atlas.service"
local_url=http://127.0.0.1:8080

usage() {
  printf 'Usage: %s {install|serve|check}\n' "$0" >&2
  exit 2
}

require_command() {
  command -v "$1" >/dev/null || { printf 'Missing command: %s\n' "$1" >&2; exit 1; }
}

tailnet_ready() {
  local status
  status=$(tailscale status --json)
  python3 -c 'import json,sys; data=json.load(sys.stdin); sys.exit(0 if data.get("BackendState")=="Running" else 1)' <<<"$status" || {
    printf 'Tailscale is not connected to a tailnet.\n' >&2
    exit 1
  }
}

# Returns 3 for no Serve mapping; rejects unrelated or public mappings.
serve_origin() {
  python3 -c '
import json,sys
data=json.load(sys.stdin)
if data.get("AllowFunnel"):
    sys.exit("Funnel is enabled; Atlas must remain tailnet-only.")
web=data.get("Web") or {}
tcp=data.get("TCP") or {}
if not web and not tcp:
    sys.exit(3)
if len(web)!=1 or len(tcp)!=1 or not (tcp.get("443") or {}).get("HTTPS"):
    sys.exit("An existing Serve mapping needs manual review; leaving it unchanged.")
host,site=next(iter(web.items()))
if not host.endswith(":443") or site.get("Handlers")!={"/":{"Proxy":"http://127.0.0.1:8080"}}:
    sys.exit("An existing Serve mapping needs manual review; leaving it unchanged.")
print("https://"+host.removesuffix(":443"))
'
}

current_origin() {
  local status
  status=$(tailscale serve status --json)
  serve_origin <<<"$status"
}

install_service() {
  require_command go
  require_command install
  require_command systemctl
  require_command curl
  local build_dir
  build_dir=$(mktemp -d)
  trap 'rm -rf -- "$build_dir"' EXIT
  (cd "$repo_root" && go build -o "$build_dir/atlas-server" ./cmd/atlas)
  install -d -m 0700 "$HOME/.local/bin" "$HOME/.local/share/atlas" "$HOME/.config/atlas"
  install -d -m 0755 "$HOME/.config/systemd/user"
  install -m 0755 "$build_dir/atlas-server" "$HOME/.local/bin/atlas-server"
  install -m 0644 "$service_file" "$HOME/.config/systemd/user/atlas.service"
  systemctl --user daemon-reload
  systemctl --user enable --now atlas.service
  systemctl --user restart atlas.service
  local ready=false
  for _ in {1..20}; do
    if curl --fail --silent --max-time 2 "$local_url/healthz" >/dev/null; then
      ready=true
      break
    fi
    sleep 0.25
  done
  [[ $ready == true ]] || { printf 'atlas.service did not become healthy.\n' >&2; exit 1; }
  printf 'Atlas is running on %s with its database at ~/.local/share/atlas/atlas.db.\n' "$local_url"
}

configure_serve() {
  require_command tailscale
  require_command python3
  tailnet_ready
  local origin code
  if origin=$(current_origin); then
    printf 'Existing private Serve mapping: %s\n' "$origin"
    return
  else
    code=$?
  fi
  if [[ $code != 3 ]]; then
    printf 'Could not confirm an empty Serve configuration; no mapping was changed.\n' >&2
    exit 1
  fi
  tailscale serve --bg 8080
  origin=$(current_origin)
  printf 'Private Atlas URL: %s\n' "$origin"
}

check_setup() {
  require_command tailscale
  require_command python3
  require_command curl
  require_command systemctl
  tailnet_ready
  systemctl --user is-active --quiet atlas.service || { printf 'atlas.service is not active.\n' >&2; exit 1; }
  curl --fail --silent --show-error --max-time 10 "$local_url/healthz" >/dev/null
  local origin
  origin=$(current_origin)
  curl --fail --silent --show-error --max-time 10 "$origin/healthz" >/dev/null
  curl --fail --silent --show-error --max-time 10 "$origin/api/v1/tasks" >/dev/null
  printf 'Atlas is reachable through Tailscale Serve at %s\n' "$origin"
}

[[ $# == 1 ]] || usage
case "$1" in
  install) install_service ;;
  serve) configure_serve ;;
  check) check_setup ;;
  *) usage ;;
esac
