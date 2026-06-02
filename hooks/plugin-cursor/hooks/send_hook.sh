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

hook_hostname=$(hostname 2>/dev/null || true)
hook_hostname_header=()
if [ -n "$hook_hostname" ]; then
  hook_hostname_header=(-H "X-Gram-Hook-Hostname: ${hook_hostname}")
fi

curl -s -X POST \
  -H "Content-Type: application/json" \
  -H "Gram-Key: ${api_key}" \
  -H "Gram-Project: ${project_slug}" \
  ${hook_hostname_header[@]+"${hook_hostname_header[@]}"} \
  -d @- \
  --max-time 10 \
  "${server_url}/rpc/hooks.cursor" 2>/dev/null || echo '{}'
