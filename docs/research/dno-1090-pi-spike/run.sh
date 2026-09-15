#!/usr/bin/env bash
# DNO-1090 spike harness. Drives real Pi headless against a Gram-shaped MCP
# server and asserts each case. See README.md.
set -uo pipefail

cd "$(dirname "$0")"
ROOT="$PWD"
WORK="$(mktemp -d)"
PI="$ROOT/node_modules/.bin/pi"
MCP_PORT=8931
PASS=0
FAIL=0

if [[ ! -x "$PI" ]]; then
  echo "pi not installed: run 'pnpm install --ignore-workspace' in $ROOT first" >&2
  exit 1
fi

cleanup() {
  for pid in "${PIDS[@]:-}"; do kill "$pid" 2>/dev/null; done
  rm -rf "$WORK"
}
trap cleanup EXIT
PIDS=()

# A stale stand-in server on one of these ports silently answers for the one
# this run meant to start, which shows up as a baffling assertion failure.
for port in "$MCP_PORT" 8932 8933 8934; do
  if (exec 3<>"/dev/tcp/127.0.0.1/$port") 2>/dev/null; then
    echo "port $port is already in use; stop the process holding it first" >&2
    exit 1
  fi
done

start_model() {
  # $1 port, $2 scripted tool name, $3 scripted arguments
  SCRIPT_TOOL="$2" SCRIPT_ARGS="$3" PORT="$1" node harness/scripted-model.mjs >"$WORK/model-$1.log" 2>&1 &
  PIDS+=("$!")
}

PORT="$MCP_PORT" node harness/gram-mcp-server.mjs >"$WORK/mcp.log" 2>&1 &
PIDS+=("$!")
start_model 8932 crm_create_task '{"project":"Apollo Migration","title":"Wire up Gram"}'
start_model 8933 bash '{"command":"echo EXFILTRATED"}'
start_model 8934 mcp__crm__crm_create_task '{"project":"Apollo Migration","title":"Wire up Gram"}'
sleep 1.5

server_calls() { curl -s "localhost:$MCP_PORT/calls" | node -e 'let s="";process.stdin.on("data",d=>s+=d).on("end",()=>console.log(JSON.parse(s).length))'; }

# run <name> <bridge> <config> <model-port> <expect: registered|failed|blocked> [env assignments...]
run_case() {
  local name="$1" bridge="$2" config="$3" port="$4" expect="$5"; shift 5
  local audit="$WORK/audit.ndjson"
  : >"$audit"
  local before after
  before="$(server_calls)"

  env "$@" \
    GRAM_MCP_CONFIG="$config" \
    GRAM_SPIKE_AUDIT="$audit" \
    SCRIPTED_MODEL_URL="http://127.0.0.1:$port/v1" \
    "$PI" -p --no-extensions \
    -e ./harness/scripted-provider.ts -e "$bridge" \
    --provider scripted --model scripted-1 --no-session \
    "do the thing" >"$WORK/pi.out" 2>&1
  after="$(server_calls)"

  local kinds delta
  kinds="$(node -e 'const fs=require("fs");const l=fs.readFileSync(process.argv[1],"utf8").trim().split("\n").filter(Boolean).map(x=>JSON.parse(x).kind);console.log(l.join(","))' "$audit")"
  delta=$((after - before))

  local ok=1
  case "$expect" in
    registered) [[ "$kinds" == *registered* && "$kinds" == *tool_result* && "$delta" -eq 1 ]] || ok=0 ;;
    failed)     [[ "$kinds" == *connect_failed* && "$delta" -eq 0 ]] || ok=0 ;;
    blocked)    [[ "$kinds" == *blocked* && "$delta" -eq 0 ]] || ok=0 ;;
  esac

  if [[ "$ok" == 1 ]]; then
    PASS=$((PASS + 1))
    printf 'PASS  %-56s audit=[%s] server_calls=+%s\n' "$name" "$kinds" "$delta"
  else
    FAIL=$((FAIL + 1))
    printf 'FAIL  %-56s audit=[%s] server_calls=+%s (wanted %s)\n' "$name" "$kinds" "$delta" "$expect"
    sed 's/^/        /' "$WORK/pi.out"
  fi
}

# Gram's generated configs, verbatim from server/internal/plugins/generate.go,
# with the MCP URL pointed at the local stand-in server.
CLAUDE=gram-claude-dialect.mcp.json
OPENCODE=gram-opencode-dialect.mcp.json
node -e '
const fs=require("fs");
const doc=JSON.parse(fs.readFileSync(process.argv[1],"utf8"));
const noauth=structuredClone(doc); delete noauth.mcpServers.crm.headers;
fs.writeFileSync(process.argv[2]+"/noauth.json", JSON.stringify(noauth,null,2));
const envref=structuredClone(doc); envref.mcpServers.crm.headers.Authorization="Bearer ${env:GRAM_API_KEY}";
fs.writeFileSync(process.argv[2]+"/envref.json", JSON.stringify(envref,null,2));
' "$CLAUDE" "$WORK"

BRIDGE=./extensions/gram-bridge.ts
SDK=./extensions/gram-bridge-sdk.ts

run_case "A  Claude-dialect config, fetch-only bridge"        "$BRIDGE"  "$CLAUDE"          8932 registered
run_case "B  OpenCode-dialect config (mcp key)"               "$BRIDGE"  "$OPENCODE"        8932 registered
run_case "C  \${env:GRAM_API_KEY} resolved from environment"   "$BRIDGE"  "$WORK/envref.json" 8932 registered GRAM_API_KEY=gram-consumer-key
run_case "D  \${env:GRAM_API_KEY} unset, header dropped"       "$BRIDGE"  "$WORK/envref.json" 8932 failed
run_case "E  credential stripped from config"                 "$BRIDGE"  "$WORK/noauth.json" 8932 failed
run_case "F  Gram policy denies the bridged tool"             "$BRIDGE"  "$CLAUDE"          8932 blocked GRAM_SPIKE_DENY=crm_create_task
run_case "G  Gram policy denies Pi's built-in bash"           "$BRIDGE"  "$CLAUDE"          8933 blocked GRAM_SPIKE_DENY=bash
run_case "H  MCP SDK bridge variant"                          "$SDK"     "$CLAUDE"          8932 registered
run_case "J  mcp__server__tool naming that toolref parses"    "$BRIDGE"  "$CLAUDE"          8934 registered GRAM_SPIKE_NAME_STYLE=mcp-prefixed

# I: the same bridge delivered as an installed pi package rather than via -e,
# proving Gram's existing GitHub publish channel can carry it.
PKG="$WORK/acme-gram-pi"
mkdir -p "$PKG/extensions"
cp extensions/gram-bridge.ts "$PKG/extensions/gram.ts"
cp "$CLAUDE" "$PKG/mcp.json"
cat >"$PKG/package.json" <<'JSON'
{
  "name": "acme-gram-pi",
  "version": "0.0.1",
  "keywords": ["pi-package"],
  "peerDependencies": { "@earendil-works/pi-coding-agent": "*", "typebox": "*" },
  "pi": { "extensions": ["./extensions"] }
}
JSON
export PI_CODING_AGENT_DIR="$WORK/pi-home"
mkdir -p "$PI_CODING_AGENT_DIR"
"$PI" install "$PKG" >"$WORK/install.log" 2>&1
: >"$WORK/audit.ndjson"
before="$(server_calls)"
# Run from an unrelated cwd: the package must resolve its own mcp.json.
(cd "$WORK" && GRAM_SPIKE_AUDIT="$WORK/audit.ndjson" SCRIPTED_MODEL_URL=http://127.0.0.1:8932/v1 \
  "$PI" -p -e "$ROOT/harness/scripted-provider.ts" --provider scripted --model scripted-1 --no-session "do the thing" \
  >"$WORK/pi.out" 2>&1)
delta=$(( $(server_calls) - before ))
if grep -q '"kind":"registered"' "$WORK/audit.ndjson" && [[ "$delta" -eq 1 ]]; then
  PASS=$((PASS + 1)); printf 'PASS  %-56s server_calls=+%s\n' "I  installed pi package, auto-loaded from other cwd" "$delta"
else
  FAIL=$((FAIL + 1)); printf 'FAIL  %-56s server_calls=+%s\n' "I  installed pi package, auto-loaded from other cwd" "$delta"; sed 's/^/        /' "$WORK/pi.out"
fi

echo
echo "$PASS passed, $FAIL failed"
[[ "$FAIL" -eq 0 ]]
