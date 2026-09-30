#!/usr/bin/env bash
# Cursor Cloud start: core infra + login-path daemons only.
# Do not run `./zero --agent` here — that pulls optional images and starts
# every pitchfork daemon, which fills a 21GB VM.
set -euo pipefail
export PATH="$HOME/.local/bin:$PATH"
cd /workspace

# shellcheck source=.cursor/ensure-dockerd.sh
. "$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/ensure-dockerd.sh"

ensure_dockerd
eval "$(mise activate bash)"

# Core images only. Presidio and LGTM are opt-in profiles and must not be
# pulled onto a 21GB VM. Install cannot start dockerd, so this is the first
# chance to cache the images for infra:start.
docker compose pull
docker compose -f compose.shared.yml -p gram-shared pull pubsub-emulator

if [ -z "${ATLAS_TOKEN:-}" ]; then
  echo "ATLAS_TOKEN is required to log in to Atlas Pro" >&2
  exit 1
fi
atlas login --token "${ATLAS_TOKEN}"
atlas whoami >/dev/null

if [ ! -f mise.local.toml ]; then
  printf '%s\n' '# Local mise configuration' '' '[tools]' '' '[env]' >mise.local.toml
fi
if ! grep -q 'USE_RECOMMENDED_SKILLS' mise.local.toml; then
  mise set --file mise.local.toml USE_RECOMMENDED_SKILLS=false
fi

# Idempotent: install already wrote these into the snapshot.
mise run zero:devidp
mise run zero:encryption
mise run zero:tunnel-identity
mise run zero:tls

# Core containers only. LGTM stays down unless a later command opts in
# with `mise run infra:lgtm`.
INFRA_READINESS_TIMEOUT="${INFRA_READINESS_TIMEOUT:-300}" mise run infra:start

mise run zero:migrations
mise run db:migrate
mise run clickhouse:migrate
mise run seed

# Login path only. Worker/streams/admin/assistant-runtime stay off so the
# disk and CPU budget goes to the dashboard.
pitchfork supervisor start
pitchfork start dev-idp server dashboard

eval "$(mise activate bash)"
pitchfork list --json --project --status failed --status errored | jq -e 'length == 0'
