#!/usr/bin/env bash
# Shared script to send hook events to Gram.
# For synchronous hooks (UserPromptSubmit, PreToolUse), the exit code
# determines whether the action is allowed:
#   0 = allow
#   2 = block (deny)

server_url="${GRAM_HOOKS_SERVER_URL:-https://app.getgram.ai}"

response=$(curl -s -w "\n%{http_code}" -X POST \
  -H "Content-Type: application/json" \
  -d @- \
  --max-time 10 \
  "${server_url}/rpc/hooks.claude")

http_code=$(echo "$response" | tail -1)
body=$(echo "$response" | sed '$d')

# Output the response body for Claude Code to read
echo "$body"

# Server returned an error (blocked by risk policy or other error)
if [ "$http_code" -ge 400 ]; then
  exit 2
fi

# Check if the response contains a deny decision
if echo "$body" | grep -q '"permissionDecision":"deny"'; then
  exit 2
fi

exit 0
