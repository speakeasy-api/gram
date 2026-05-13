#!/usr/bin/env bash

# SessionStart-specific hook: enriches the payload with the active MCP
# server list (via `claude mcp list`) and forwards it to Gram. Runs async,
# so we fire-and-forget — SessionStart has no allow/deny decision to honor.

set -u

server_url="${GRAM_HOOKS_SERVER_URL:-https://app.getgram.ai}"

payload=$(cat)

# `claude mcp list` health-checks every server, which can take seconds for
# stdio servers. Hard-cap wall time so a misbehaving server can't keep this
# hook alive forever; since the hook is async the latency is invisible to
# Claude anyway. macOS doesn't ship GNU `timeout` — prefer it, fall back to
# coreutils' `gtimeout`, then to no timeout at all rather than failing.
mcp_output=""
if command -v claude >/dev/null 2>&1; then
  if command -v timeout >/dev/null 2>&1; then
    mcp_output=$(timeout 15 claude mcp list 2>&1 || true)
  elif command -v gtimeout >/dev/null 2>&1; then
    mcp_output=$(gtimeout 15 claude mcp list 2>&1 || true)
  else
    mcp_output=$(claude mcp list 2>&1 || true)
  fi
fi

enriched=$(MCP_OUT="$mcp_output" PAYLOAD="$payload" python3 -c '
import json, os, sys
try:
    p = json.loads(os.environ["PAYLOAD"])
except Exception:
    sys.exit(1)
ad = p.get("additional_data") or {}
ad["mcp_list_output"] = os.environ.get("MCP_OUT", "")
p["additional_data"] = ad
print(json.dumps(p))
') || enriched="$payload"

curl -s -o /dev/null -X POST \
  -H "Content-Type: application/json" \
  -d "$enriched" \
  --max-time 30 \
  "${server_url}/rpc/hooks.claude" >/dev/null 2>&1 || true

exit 0
