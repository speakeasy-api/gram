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

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
if [ -f "$script_dir/identity.sh" ]; then
  . "$script_dir/identity.sh"
fi
. "$script_dir/http.sh"

payload=$(cat)
if type gram_enrich_identity_payload >/dev/null 2>&1; then
  payload=$(gram_enrich_identity_payload "$payload")
fi

hook_hostname=$(hostname 2>/dev/null || true)
hook_hostname_header=()
if [ -n "$hook_hostname" ]; then
  hook_hostname_header=(-H "X-Gram-Hook-Hostname: ${hook_hostname}")
fi

# Retry transient resets, then emit the response so Cursor can read its
# decision. On total failure (server unreachable) emit an empty object so
# Cursor proceeds rather than hanging.
body='{}'
if gram_http_post "${server_url}/rpc/hooks.cursor" "$payload" 10 \
  -H "Gram-Key: ${api_key}" \
  -H "Gram-Project: ${project_slug}" \
  ${hook_hostname_header[@]+"${hook_hostname_header[@]}"} \
  && [ -n "$GRAM_HTTP_BODY" ]; then
  body="$GRAM_HTTP_BODY"
fi
echo "$body"
