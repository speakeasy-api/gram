#!/usr/bin/env bash
# DNO-1090 spike: prints the tool list Pi actually offers the model under each
# distribution option, from the same Gram-generated mcp.json. The scripted
# model rejects an unknown tool name and echoes the list it was offered, so
# this is read straight off the provider request rather than inferred.
set -uo pipefail

cd "$(dirname "$0")"
ROOT="$PWD"
WORK="$(mktemp -d)"
PI="$ROOT/node_modules/.bin/pi"
MCP_PORT=8931
PROBE_PORT=8936
CONFIG="$ROOT/gram-claude-dialect.mcp.json"

if [[ ! -x "$PI" ]]; then
  echo "pi not installed: run 'pnpm install --ignore-workspace' in $ROOT first" >&2
  exit 1
fi

PIDS=()
cleanup() {
  for pid in "${PIDS[@]:-}"; do kill "$pid" 2>/dev/null; done
  rm -rf "$WORK"
}
trap cleanup EXIT

PORT="$MCP_PORT" node harness/gram-mcp-server.mjs >"$WORK/mcp.log" 2>&1 &
PIDS+=("$!")
SCRIPT_TOOL=__probe__ PORT="$PROBE_PORT" node harness/scripted-model.mjs >"$WORK/model.log" 2>&1 &
PIDS+=("$!")
sleep 1.5

# $1 label, $2 pi home, remaining: extra pi args
probe() {
  local label="$1" home="$2"; shift 2
  local raw
  raw="$(PI_CODING_AGENT_DIR="$home" \
    GRAM_MCP_CONFIG="$CONFIG" \
    GRAM_SPIKE_AUDIT="$WORK/audit.ndjson" \
    SCRIPTED_MODEL_URL="http://127.0.0.1:$PROBE_PORT/v1" \
    "$PI" -p -e "$ROOT/harness/scripted-provider.ts" "$@" \
    --provider scripted --model scripted-1 --no-session "list crm projects" 2>&1 | tail -3)"
  printf '%s\n    %s\n\n' "$label" "$(sed -n 's/.*absent from tool list: \[\(.*\)\].*/\1/p' <<<"$raw")"
}

echo "Gram config in play: gram-claude-dialect.mcp.json, emitted by server/internal/plugins"
echo

# Case 1 gives stock Pi every chance to find the config: a copy in its own
# agent dir and one at the project root, with no extension loaded.
BARE="$WORK/pi-bare"
mkdir -p "$BARE"
cp "$CONFIG" "$BARE/mcp.json"
cp "$CONFIG" "$WORK/.mcp.json"
(cd "$WORK" && PI_CODING_AGENT_DIR="$BARE" SCRIPTED_MODEL_URL="http://127.0.0.1:$PROBE_PORT/v1" \
  "$PI" -p --no-extensions -e "$ROOT/harness/scripted-provider.ts" \
  --provider scripted --model scripted-1 --no-session "list crm projects" 2>&1 | tail -3) \
  | sed -n 's/.*absent from tool list: \[\(.*\)\].*/1. Pi as shipped, Gram config in ~\/.pi\/agent\/mcp.json and .\/.mcp.json:\n    \1\n/p'

EMPTY="$WORK/pi-empty"
mkdir -p "$EMPTY"
probe "2. Gram-owned bridge extension (this spike):" "$EMPTY" --no-extensions -e "$ROOT/extensions/gram-bridge.ts"

# Community adapter gets its own pi home so it cannot leak into the other
# cases. It reads <pi home>/mcp.json, so Gram's file is copied there.
ADAPTER="$WORK/pi-adapter"
mkdir -p "$ADAPTER"
cp "$CONFIG" "$ADAPTER/mcp.json"
PI_CODING_AGENT_DIR="$ADAPTER" "$PI" install npm:pi-mcp-adapter >"$WORK/install.log" 2>&1 || {
  echo "pi-mcp-adapter install failed; see $WORK/install.log" >&2
  exit 1
}
probe "3. Community pi-mcp-adapter, Gram config unmodified:" "$ADAPTER"

node -e '
const fs=require("fs");const p=process.argv[1];
const doc=JSON.parse(fs.readFileSync(p,"utf8"));
doc.mcpServers.crm.directTools=["crm_list_projects","crm_create_task"];
fs.writeFileSync(p,JSON.stringify(doc,null,2));
' "$ADAPTER/mcp.json"
probe "4. Community pi-mcp-adapter + its own directTools key:" "$ADAPTER"
