#!/usr/bin/env bash
set -euo pipefail

root=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/../.." && pwd)
setup="$root/deploy/apex/setup.sh"
fixtures="$root/deploy/apex/testdata"
scratch=$(mktemp -d)
trap 'rm -rf -- "$scratch"' EXIT
mkdir -p "$scratch/bin"
export ATLAS_SERVE_STATE="$scratch/serve.json"
export ATLAS_SERVE_TARGET="$fixtures/serve-existing.json"
export ATLAS_MOCK_LOG="$scratch/calls.log"
export PATH="$scratch/bin:$PATH"

cat >"$scratch/bin/tailscale" <<'MOCK'
#!/usr/bin/env bash
case "$*" in
  'status --json') printf '{"BackendState":"%s"}\n' "${ATLAS_TAILSCALE_STATE:-Running}" ;;
  'serve status --json') cat "$ATLAS_SERVE_STATE" ;;
  'serve --bg 8080') printf 'serve configured\n' >>"$ATLAS_MOCK_LOG"; cp "$ATLAS_SERVE_TARGET" "$ATLAS_SERVE_STATE" ;;
  *) exit 1 ;;
esac
MOCK
cat >"$scratch/bin/systemctl" <<'MOCK'
#!/usr/bin/env bash
case "$*" in
  '--user is-active --quiet atlas.service'|'--user daemon-reload'|'--user enable --now atlas.service'|'--user restart atlas.service') ;;
  *) exit 1 ;;
esac
MOCK
cat >"$scratch/bin/go" <<'MOCK'
#!/usr/bin/env bash
[[ "$1" == build && "$2" == -o && "$4" == ./cmd/atlas ]] || exit 1
printf '#!/usr/bin/env bash\n' >"$3"
MOCK
cat >"$scratch/bin/curl" <<'MOCK'
#!/usr/bin/env bash
printf '%s\n' "${@: -1}" >>"$ATLAS_MOCK_LOG"
MOCK
chmod +x "$scratch/bin/tailscale" "$scratch/bin/systemctl" "$scratch/bin/go" "$scratch/bin/curl"

cp "$fixtures/serve-existing.json" "$ATLAS_SERVE_STATE"
output=$(bash "$setup" serve)
[[ "$output" == *'https://atlas.example.ts.net'* ]]
[[ ! -e "$ATLAS_MOCK_LOG" ]]

cp "$fixtures/serve-empty.json" "$ATLAS_SERVE_STATE"
output=$(bash "$setup" serve)
[[ "$output" == *'https://atlas.example.ts.net'* ]]
[[ $(rg -c 'serve configured' "$ATLAS_MOCK_LOG") == 1 ]]

for fixture in serve-unrelated.json serve-funnel.json; do
  cp "$fixtures/$fixture" "$ATLAS_SERVE_STATE"
  if bash "$setup" serve >"$scratch/output" 2>&1; then
    printf 'Accepted an unsafe Serve mapping: %s\n' "$fixture" >&2
    exit 1
  fi
  [[ $(rg -c 'serve configured' "$ATLAS_MOCK_LOG") == 1 ]]
done

cp "$fixtures/serve-existing.json" "$ATLAS_SERVE_STATE"
if ATLAS_TAILSCALE_STATE=Stopped bash "$setup" serve >"$scratch/output" 2>&1; then
  printf 'Accepted a disconnected tailnet.\n' >&2
  exit 1
fi

output=$(bash "$setup" check)
[[ "$output" == *'https://atlas.example.ts.net'* ]]
rg -q '^http://127.0.0.1:8080/healthz$' "$ATLAS_MOCK_LOG"
rg -q '^https://atlas.example.ts.net/healthz$' "$ATLAS_MOCK_LOG"
rg -q '^https://atlas.example.ts.net/api/v1/tasks$' "$ATLAS_MOCK_LOG"
output=$(HOME="$scratch/home" bash "$setup" install)
[[ "$output" == *'Atlas is running on http://127.0.0.1:8080'* ]]
[[ -x "$scratch/home/.local/bin/atlas-server" ]]
[[ -f "$scratch/home/.config/systemd/user/atlas.service" ]]
printf 'Apex Serve fixtures passed.\n'
