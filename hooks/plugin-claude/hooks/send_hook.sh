#!/usr/bin/env bash
# Shared script to send hook events to Gram

server_url="${GRAM_HOOKS_SERVER_URL:-https://app.getgram.ai}"

curl -X POST \
  -H "Content-Type: application/json" \
  -d @- \
  --max-time 30 \
  "${server_url}/rpc/hooks.claude"
