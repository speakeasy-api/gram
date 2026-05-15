#!/usr/bin/env bash

# SessionStart-specific hook: enriches the payload with the active MCP
# server list and forwards it to Gram. Runs async, so we fire-and-forget —
# SessionStart has no allow/deny decision to honor.
#
# Two execution environments are supported:
#   - cowork: detected by the presence of cmux's per-run local_<rid>.json
#     config file. We extract its remoteMcpServersConfig (connector UUID +
#     URL pairs) and ship them as mcp_inventory_cowork.
#   - Claude Code (default): shell out to `claude mcp list` and forward
#     the human-readable output as mcp_inventory_claude_code.

set -u

server_url="${GRAM_HOOKS_SERVER_URL:-https://app.getgram.ai}"

payload=$(cat)

mcp_inventory_claude_code=""
mcp_inventory_cowork="null"

# Locate cmux's per-run config file. CLAUDE_PROJECT_DIR is
# .../local_<rid>/outputs; the config sits one directory up as
# .../local_<rid>.json and lists the remote MCP connectors with their
# connector UUIDs. That's the only spot on the host filesystem where the
# UUID <-> URL pairing exists, so when we find it we ship it verbatim.
local_run_json=""

if [ -n "${CLAUDE_PROJECT_DIR:-}" ]; then
  candidate_local_dir=$(dirname "$CLAUDE_PROJECT_DIR")
  candidate_local_json="${candidate_local_dir}.json"
  if [ -f "$candidate_local_json" ]; then
    local_run_json="$candidate_local_json"
  else
    # SessionStart often fires before cmux writes the per-run config
    # file. Fall back to the most-recent sibling local_*.json — the
    # remoteMcpServersConfig block is account/org-scoped and identical
    # across runs in the same subid directory, so any sibling is good
    # enough for the UUID <-> URL mapping we care about.
    parent_dir=$(dirname "$candidate_local_dir")
    if [ -d "$parent_dir" ]; then
      sibling=$(ls -t "$parent_dir"/local_*.json 2>/dev/null | head -1)
      if [ -n "$sibling" ] && [ -f "$sibling" ]; then
        local_run_json="$sibling"
      fi
    fi
  fi
fi

if [ -n "$local_run_json" ] && command -v jq >/dev/null 2>&1; then
  # Extract the connector UUID + URL pairs we actually care about.
  # `tools` is dropped — it can be huge and we don't need it here.
  inv=$(jq -c '
    [
      (.remoteMcpServersConfig // [])[]
      | {
          connector_uuid: .uuid,
          name:           .name,
          url:            .url,
          source:         "claude.ai"
        }
    ]
  ' "$local_run_json" 2>/dev/null)
  [ -n "$inv" ] && mcp_inventory_cowork="$inv"
elif command -v claude >/dev/null 2>&1; then
  # Claude Code: `claude mcp list` health-checks every server, which can
  # take seconds for stdio servers. Hard-cap wall time so a misbehaving
  # server can't keep this hook alive forever; since the hook is async
  # the latency is invisible to Claude anyway. macOS doesn't ship GNU
  # `timeout` — prefer it, fall back to coreutils' `gtimeout`, then to
  # no timeout at all rather than failing.
  if command -v timeout >/dev/null 2>&1; then
    mcp_inventory_claude_code=$(timeout 15 claude mcp list 2>&1 || true)
  elif command -v gtimeout >/dev/null 2>&1; then
    mcp_inventory_claude_code=$(gtimeout 15 claude mcp list 2>&1 || true)
  else
    mcp_inventory_claude_code=$(claude mcp list 2>&1 || true)
  fi
fi

enriched=$(MCP_CC="$mcp_inventory_claude_code" \
           MCP_CW="$mcp_inventory_cowork" \
           PAYLOAD="$payload" \
           python3 -c '
import json, os, sys
try:
    p = json.loads(os.environ["PAYLOAD"])
except Exception:
    sys.exit(1)
ad = p.get("additional_data") or {}
cc = os.environ.get("MCP_CC", "")
if cc:
    ad["mcp_inventory_claude_code"] = cc
try:
    cw = json.loads(os.environ.get("MCP_CW", "null"))
except Exception:
    cw = None
if cw is not None:
    ad["mcp_inventory_cowork"] = cw
p["additional_data"] = ad
print(json.dumps(p))
') || enriched="$payload"

curl -s -o /dev/null -X POST \
  -H "Content-Type: application/json" \
  -d "$enriched" \
  --max-time 30 \
  "${server_url}/rpc/hooks.claude" >/dev/null 2>&1 || true

exit 0
