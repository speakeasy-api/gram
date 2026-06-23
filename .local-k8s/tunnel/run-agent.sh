#!/usr/bin/env bash
# Run the tunnel agent as a STANDALONE executable next to your MCP server.
# The agent is the customer-side component — outbound-only, NOT a cluster
# workload. It dials the gateway (deployed in k8s) and reverse-proxies to one
# pinned local MCP URL.
#
# Usage:
#   TUNNEL_LOCAL_MCP_URL=http://localhost:9000 ./.local-k8s/tunnel/run-agent.sh
#
# Override any of the env vars below. Defaults target the local kind gateway via
# its ingress host (needs `127.0.0.1 tunnel.gram.local` in /etc/hosts) and the
# demo seed key from 10-gateway.yaml.
set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"

export TUNNEL_GATEWAY_URL="${TUNNEL_GATEWAY_URL:-ws://tunnel.gram.local/connect}"
export TUNNEL_KEY="${TUNNEL_KEY:-gram_tunnel_localpocdemokey000000000000000000000000}"
export TUNNEL_LOCAL_MCP_URL="${TUNNEL_LOCAL_MCP_URL:-}"

if [ -z "$TUNNEL_LOCAL_MCP_URL" ]; then
  echo "set TUNNEL_LOCAL_MCP_URL to your MCP server (reachable from this host)" >&2
  exit 2
fi

echo "tunnel-agent -> gateway=$TUNNEL_GATEWAY_URL  mcp=$TUNNEL_LOCAL_MCP_URL"
cd "$REPO_ROOT"
exec go run ./tunnel/cmd/tunnel-agent
