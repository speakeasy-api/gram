#!/usr/bin/env bash
# Start Codex with the two Ask POC MCP servers in an empty folder, with the
# first prompt already sent. Every other configured MCP server whose name
# contains "slack" is disabled for this run, so the model can only reach the
# mock.
#
# Runs with --dangerously-bypass-approvals-and-sandbox so Codex never stops
# to ask (MCP calls, writing the watcher, curl to 127.0.0.1). Commands run
# unsandboxed on this machine, so only use it with this mock setup.
set -euo pipefail
PLAY="$HOME/.cache/ask-spike/playground"
mkdir -p "$PLAY"
cd "$PLAY"
DEFAULT_PROMPT=$(
  cat <<'EOF'
The customer asked for an update on their failed export in #ext-support. We're still investigating, so post a holding reply there with the ask_poc_slack send_message tool: "Hi team, quick update from the FDE side: we're looking into your export issue and will follow up shortly."
EOF
)
PROMPT="${1:-$DEFAULT_PROMPT}"

args=(
  -c 'mcp_servers.ask_poc_slack.url="http://127.0.0.1:4701/mcp"'
  -c 'mcp_servers.ask_poc_approvals.url="http://127.0.0.1:4702/mcp"'
  --dangerously-bypass-approvals-and-sandbox
)
# Disable real Slack servers: each needs its url restated for the override to parse.
while IFS=$'\t' read -r name url; do
  [ -n "$name" ] || continue
  args+=(-c "mcp_servers.${name}.url=\"${url}\"" -c "mcp_servers.${name}.enabled=false")
done < <(codex mcp list --json 2>/dev/null | node -e '
  let s = ""; process.stdin.on("data", (c) => (s += c)).on("end", () => {
    for (const x of JSON.parse(s)) {
      if (/slack/i.test(x.name) && !x.name.startsWith("ask_poc") && x.transport?.url) console.log(`${x.name}\t${x.transport.url}`);
    }
  });')

exec codex "${args[@]}" "$PROMPT"
