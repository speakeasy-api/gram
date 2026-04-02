#!/usr/bin/env bash
# Shared script to send Cursor hook events to Gram

server_url="${GRAM_HOOKS_SERVER_URL:-https://app.getgram.ai}"
api_key="${GRAM_API_KEY:-}"
project_slug="${GRAM_PROJECT_SLUG:-}"

# Fail silently if credentials are not configured
if [ -z "$api_key" ] || [ -z "$project_slug" ]; then
  echo '{}'
  exit 0
fi

curl -s -X POST \
  -H "Content-Type: application/json" \
  -H "Gram-Key: ${api_key}" \
  -H "Gram-Project: ${project_slug}" \
  -d @- \
  --max-time 10 \
  "${server_url}/rpc/hooks.cursor" 2>/dev/null || echo '{}'
