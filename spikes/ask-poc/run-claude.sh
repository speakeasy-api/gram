#!/usr/bin/env bash
# Start Claude Code with the Ask POC plugin in an empty folder, with the first
# prompt already sent. Real Slack tools are hidden so the model can only reach
# the mock. Pass your own prompt as the first argument to override.
#
# The two POC tools are allowed client-side: the approvals server is the
# authority for them. Without the allow rule, auto mode's classifier can block
# the retry that a background Monitor event triggers. ASK_POC_ALLOW_TOOLS=0
# leaves the rule out, to reproduce that.
set -euo pipefail
DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PLAY="$HOME/.cache/ask-spike/playground"
mkdir -p "$PLAY"
cd "$PLAY"
DEFAULT_PROMPT=$(
  cat <<'EOF'
The customer asked for an update on their failed export in #ext-support. We're still investigating, so post a holding reply there with the external-slack send_message tool: "Hi team, quick update from the FDE side: we're looking into your export issue and will follow up shortly."
EOF
)
PROMPT="${1:-$DEFAULT_PROMPT}"
args=(--plugin-dir "$DIR/plugin" --disallowedTools 'mcp__*Slack*')
if [ "${ASK_POC_ALLOW_TOOLS:-1}" = "1" ]; then
  args+=(--allowedTools 'mcp__plugin_ask-poc_external-slack__send_message' 'mcp__plugin_ask-poc_approvals__request_approval')
fi
exec claude "$PROMPT" "${args[@]}"
