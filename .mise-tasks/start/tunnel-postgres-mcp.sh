#!/usr/bin/env bash

#MISE description="Start local Postgres MCP behind a tunnel agent"
#MISE dir="{{ config_root }}"
#MISE hide=true

set -euo pipefail

# TUNNEL_LOCAL_KEY ships as a fixed default in mise.toml and the matching
# tunnel row is written by `mise run seed`, so this only trips if someone has
# deliberately unset it.
if [[ -z "${TUNNEL_LOCAL_KEY:-}" ]]; then
  echo "TUNNEL_LOCAL_KEY is not set; skipping the local Postgres MCP tunnel." >&2
  exit 0
fi

cleanup() {
  docker compose --profile tunnel stop tunnel-agent tunnel-postgres-mcp >/dev/null 2>&1 || true
}
trap cleanup EXIT INT TERM

docker compose --profile tunnel up --build --menu=false tunnel-postgres-mcp tunnel-agent
