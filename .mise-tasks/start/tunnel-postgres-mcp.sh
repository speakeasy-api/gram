#!/usr/bin/env bash

#MISE description="Start local Postgres MCP behind a tunnel agent"
#MISE dir="{{ config_root }}"
#MISE hide=true

set -euo pipefail

source "$(dirname "${BASH_SOURCE[0]}")/../../local/lib/compose.sh"

if [[ -z "${TUNNEL_LOCAL_KEY:-}" ]]; then
  echo "TUNNEL_LOCAL_KEY is not set; skipping the local Postgres MCP tunnel. Run 'mise run seed' to enable it." >&2
  exit 0
fi

cleanup() {
  compose --profile tunnel stop tunnel-agent tunnel-postgres-mcp >/dev/null 2>&1 || true
}
trap cleanup EXIT INT TERM

# The tunnel MCP image is declared with a git URL build context, which compose
# cannot build through the podman socket (no BuildKit). Pre-build it with
# `podman build` (idempotent) and let compose consume the tagged image
# (pull_policy: never in compose.yml).
ensure_tunnel_image

# tunnel-agent reaches the tunnel gateway on the host via
# host.docker.internal:host-gateway (compose.yml). Under rootless podman+pasta
# that alias maps to the host, and the gateway listens on :8090 (all
# interfaces) — if the agent cannot connect, check that the gateway is not
# bound to 127.0.0.1 only.
compose --profile tunnel up --menu=false tunnel-postgres-mcp tunnel-agent
