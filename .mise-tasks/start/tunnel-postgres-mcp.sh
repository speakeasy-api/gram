#!/usr/bin/env bash

#MISE description="Start local Postgres MCP behind a tunnel agent"
#MISE dir="{{ config_root }}"

set -euo pipefail

if [[ -z "${TUNNEL_LOCAL_KEY:-}" ]]; then
  echo "TUNNEL_LOCAL_KEY is not set. Run 'mise run seed' first." >&2
  exit 2
fi

# Local dogfood default: read-write Postgres MCP. Override with TUNNEL_POSTGRES_MCP_ACCESS_MODE=restricted.
export TUNNEL_POSTGRES_MCP_ACCESS_MODE="${TUNNEL_POSTGRES_MCP_ACCESS_MODE:-unrestricted}"

cleanup() {
  docker compose --profile tunnel stop tunnel-agent tunnel-postgres-mcp >/dev/null 2>&1 || true
}
trap cleanup EXIT INT TERM

docker compose --profile tunnel up --build --menu=false tunnel-postgres-mcp tunnel-agent
