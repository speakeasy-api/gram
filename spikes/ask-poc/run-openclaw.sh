#!/usr/bin/env bash
# Start OpenClaw's local terminal UI inside the Docker sandbox, with the first
# prompt already sent. The sandbox state (~/.cache/ask-spike/openclaw-state)
# has the two Ask POC MCP servers configured at host.docker.internal, exec
# approvals off and notifyOnExit on. The real ~/.openclaw is never mounted.
#
# Start the POC servers for containers first:
#   ASK_POC_WATCHER_PATH=/tmp/ask-poc/ask-watch-v1.sh ./ask-poc.sh restart
set -euo pipefail
STATE_DIR="${STATE_DIR:-$HOME/.cache/ask-spike/openclaw-state}"
IMAGE="${IMAGE:-ask-claw:2026.10.4}"
DEFAULT_PROMPT=$(
  cat <<'EOF'
The customer asked for an update on their failed export in #ext-support. We're still investigating, so post a holding reply there with the ask_poc_slack send_message tool: "Hi team, quick update from the FDE side: we're looking into your export issue and will follow up shortly."
EOF
)
PROMPT="${1:-$DEFAULT_PROMPT}"
# The model comes from the sandbox state (openclaw models set): openai/* runs on
# OpenClaw's Codex runtime with the ChatGPT sign-in, openrouter/* needs
# OPENROUTER_API_KEY. Use the repo's dev OpenRouter key unless it is already
# set. Only the variable name reaches docker, so the value never lands in argv
# or on disk.
if [ -z "${OPENROUTER_API_KEY:-}" ]; then
  OPENROUTER_API_KEY="$(python3 -I -c "import tomllib;print(tomllib.load(open('$HOME/code/gram/mise.local.toml','rb'))['env']['OPENROUTER_DEV_KEY'])")"
  export OPENROUTER_API_KEY
fi
exec docker run --rm -it --name ask-claw \
  -e OPENROUTER_API_KEY \
  -v "$STATE_DIR:/home/claw/.openclaw" \
  --add-host host.docker.internal:host-gateway \
  "$IMAGE" openclaw chat --session "ask-poc-$(date +%s)" --message "$PROMPT"
