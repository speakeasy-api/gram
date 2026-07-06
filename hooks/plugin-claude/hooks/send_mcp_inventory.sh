#!/usr/bin/env bash

# MCP inventory hook: enriches the payload with the active MCP server list
# and forwards it to Gram. Registered against both SessionStart and
# ConfigChange so the server re-syncs its cached inventory whenever Claude
# (re)loads the session or a settings file changes mid-session. Neither
# event has an allow/deny decision to honor, so we always exit 0 and
# fire-and-forget.
#
# Two execution environments are supported:
#   - cowork: detected by the presence of cmux's per-run local_<rid>.json
#     config file. We extract its remoteMcpServersConfig (connector UUID +
#     URL pairs) and ship them as mcp_inventory_cowork.
#   - Claude Code (default): shell out to `claude mcp list` and forward
#     the human-readable output as mcp_inventory_claude_code.

set -u

server_url="${GRAM_HOOKS_SERVER_URL:-https://app.getgram.ai}"

# This hook is fire-and-forget: it never blocks and always exits 0, so every
# failure is otherwise invisible. Set GRAM_HOOKS_DEBUG=1 to surface why the MCP
# inventory was never collected or delivered (e.g. "user_email not attributed"
# or "inventory never reached the server" support questions).
debug() {
  if [ -n "${GRAM_HOOKS_DEBUG:-}" ]; then
    printf 'gram-hooks(mcp-inventory): %s\n' "$1" >&2
  fi
}

hook_hostname=$(hostname 2>/dev/null || true)
hook_hostname_header=()
if [ -n "$hook_hostname" ]; then
  hook_hostname_header=(-H "X-Gram-Hook-Hostname: ${hook_hostname}")
fi
payload=$(cat)

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
if [ -f "$script_dir/identity.sh" ]; then
  . "$script_dir/identity.sh"
fi
. "$script_dir/http.sh"

api_key="${GRAM_HOOKS_API_KEY:-}"
project_slug="${GRAM_HOOKS_PROJECT_SLUG:-}"
gram_hooks_org_hint="${GRAM_HOOKS_ORG_ID:-}"

# No env key: fall back to the credentials the browser login flow cached
# (auth_preflight.sh / login.sh) so the inventory lands attributed. auth.sh
# installs an EXIT trap when sourced; the trap set below deliberately
# replaces it and covers the only resource created here (the curl config).
if [ -z "$api_key" ] && [ -f "$script_dir/auth.sh" ]; then
  # shellcheck source=/dev/null
  if . "$script_dir/auth.sh" 2>/dev/null && type gram_hooks_read_auth >/dev/null 2>&1; then
    if gram_hooks_read_auth "$server_url" 2>/dev/null; then
      api_key="$GRAM_HOOKS_CACHED_API_KEY"
      [ -n "$project_slug" ] || project_slug="$GRAM_HOOKS_CACHED_PROJECT"
    fi
  fi
fi

auth_config=""
auth_config_arg=()
cleanup_auth_config() {
  if [ -n "$auth_config" ]; then
    rm -f "$auth_config"
  fi
}
trap cleanup_auth_config EXIT
if [ -n "$api_key" ] || [ -n "$project_slug" ]; then
  auth_config=$(mktemp "${TMPDIR:-/tmp}/gram-hooks-curl.XXXXXX") || {
    debug "mktemp failed for curl auth config; skipping inventory upload"
    exit 0
  }
  chmod 600 "$auth_config" || true
  # curl config quoted strings treat backslash and double quote specially, and
  # the config file is line-oriented; escape the metacharacters and strip CR/LF
  # so a corrupted value cannot break out of the directive or inject additional
  # config lines.
  api_key="${api_key//\\/\\\\}"
  api_key="${api_key//\"/\\\"}"
  api_key="${api_key//$'\n'/}"
  api_key="${api_key//$'\r'/}"
  project_slug="${project_slug//\\/\\\\}"
  project_slug="${project_slug//\"/\\\"}"
  project_slug="${project_slug//$'\n'/}"
  project_slug="${project_slug//$'\r'/}"
  if [ -n "$api_key" ]; then
    printf 'header = "Gram-Key: %s"\n' "$api_key" >>"$auth_config"
  fi
  if [ -n "$project_slug" ]; then
    printf 'header = "Gram-Project: %s"\n' "$project_slug" >>"$auth_config"
  fi
  auth_config_arg=(--config "$auth_config")
else
  debug "no cached or env hook credentials; sending inventory unauthenticated (server will fall back to OTEL session attribution)"
fi
if type gram_enrich_identity_payload >/dev/null 2>&1; then
  payload=$(gram_enrich_identity_payload "$payload")
fi

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
  # cmux's field naming has drifted across versions (snake_case vs
  # camelCase, `uuid` vs `id` for the connector identifier) so we try
  # multiple candidates per slot and keep the first non-null. This is
  # the field that becomes the `mcp__<server>__tool` prefix server-side,
  # so getting it wrong silently shows users a UUID instead of "Slack".
  inv=$(jq -c '
    [
      (.remoteMcpServersConfig // [])[]
      | {
          connector_uuid: (.uuid // .connectorUuid // .connector_uuid // .id // .connectorId // .connector_id // null),
          name:           (.name // .displayName // .display_name // null),
          url:            (.url // .serverUrl // .server_url // null),
          source:         "claude.ai"
        }
    ]
  ' "$local_run_json" 2>/dev/null)
  if [ -n "$inv" ]; then
    mcp_inventory_cowork="$inv"
  else
    debug "jq found no remoteMcpServersConfig in $local_run_json (cowork inventory empty)"
  fi
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
  [ -z "$mcp_inventory_claude_code" ] && debug "'claude mcp list' produced no output (timed out, errored, or no servers configured)"
else
  debug "no MCP inventory source found: no cowork local_*.json reachable and no 'claude' binary on PATH"
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
') || {
  debug "python3 enrichment failed (missing or payload not JSON); sending original payload without MCP inventory"
  enriched="$payload"
}

# Fire-and-forget through the shared helper so a transient reset retries (see
# http.sh) instead of dropping the inventory. The hook never blocks, but we
# still log the delivery outcome under GRAM_HOOKS_DEBUG so support can tell
# "never reached the server" from "server rejected it".
gram_http_post "${server_url}/rpc/hooks.claude" "$enriched" 30 \
  ${hook_hostname_header[@]+"${hook_hostname_header[@]}"} \
  ${auth_config_arg[@]+"${auth_config_arg[@]}"} >/dev/null 2>&1 || true

http_code="$GRAM_HTTP_CODE"
if [ "$http_code" -ge 200 ] 2>/dev/null && [ "$http_code" -lt 300 ] 2>/dev/null; then
  debug "inventory delivered (HTTP ${http_code})"
elif [ "$http_code" = "000" ]; then
  debug "inventory NOT delivered: could not reach ${server_url} (connection failure or timeout)"
else
  debug "inventory NOT delivered: server returned HTTP ${http_code}"
fi

exit 0
