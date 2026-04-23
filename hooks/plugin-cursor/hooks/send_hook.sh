#!/usr/bin/env bash
# Shared script to send Cursor hook events to Gram

server_url="https://recent-patrica-unmonastically.ngrok-free.dev" # TODO "${GRAM_HOOKS_SERVER_URL:-https://app.getgram.ai}"
api_key="gram_local_3bfe0ef1d332662029c1acc75c4a55161415ed6e883db3dc7bf6b93de0751d4a" # TODO "${GRAM_API_KEY:-}"
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
