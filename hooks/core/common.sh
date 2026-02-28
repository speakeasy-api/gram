#!/usr/bin/env bash
# Common functions shared across Gram hook scripts
# This is the core module that can be packaged for different distributions

# Ensure standard paths are available (hooks may have minimal PATH)
export PATH="/usr/local/bin:/usr/bin:/bin:/usr/sbin:/sbin:$PATH"

# Validates required environment variables and blocks if missing
# Usage: validate_env_vars "HookEventName"
validate_env_vars() {
  local hook_event_name="$1"
  
  echo "validating env vars for $hook_event_name" >> /tmp/gram-hooks-debug.log

  if [ -z "$GRAM_API_KEY" ]; then
    echo "GRAM_API_KEY is not set" >> /tmp/gram-hooks-debug.log
    output_block_json "$hook_event_name" "GRAM_API_KEY environment variable is not set. Please set it to enable Gram hooks."
    exit 0
  fi

  # Set default project if not specified
  if [ -z "$GRAM_PROJECT" ]; then
    GRAM_PROJECT="default"
  fi
}

# Outputs blocking JSON response based on hook type
# Usage: output_block_json "HookEventName" "reason message"
output_block_json() {
  local hook_event_name="$1"
  local reason="$2"

  if [ "$hook_event_name" = "PreToolUse" ]; then
    cat << EOF
{
  "hookSpecificOutput": {
    "hookEventName": "$hook_event_name",
    "permissionDecision": "deny",
    "permissionDecisionReason": "$reason"
  }
}
EOF
  else
    cat << EOF
{
  "hookSpecificOutput": {
    "hookEventName": "$hook_event_name",
    "decision": "block",
    "reason": "$reason"
  }
}
EOF
  fi
}

# Outputs success JSON response based on hook type
# Usage: output_success_json "HookEventName"
output_success_json() {
  local hook_event_name="$1"

  if [ "$hook_event_name" = "PreToolUse" ]; then
    cat << EOF
{
  "hookSpecificOutput": {
    "hookEventName": "$hook_event_name",
    "permissionDecision": "allow"
  }
}
EOF
  else
    cat << EOF
{
  "hookSpecificOutput": {
    "hookEventName": "$hook_event_name"
  }
}
EOF
  fi
}

# Calls the Gram API and handles the response
# Usage: call_gram_api "endpoint_name" "HookEventName"
call_gram_api() {
  local endpoint="$1"
  local hook_event_name="$2"
  local server_url="$GRAM_SERVER_URL"
  if [ -z "$server_url" ]; then
    server_url="http://localhost:8080" # TODO https://app.getgram.ai"
  fi

  echo "Calling Gram API $server_url/rpc/hooks.$endpoint" >> /tmp/gram-hooks-debug.log

  INPUT=$(cat)
  HTTP_CODE=$(curl -s -w "%{http_code}" -o /tmp/gram_hook_response.json -X POST "$server_url/rpc/hooks.$endpoint" \
    -H "Content-Type: application/json" \
    -H "Gram-Key: $GRAM_API_KEY" \
    -H "Gram-Project: $GRAM_PROJECT" \
    -d "$INPUT")
  RESPONSE=$(cat /tmp/gram_hook_response.json 2>/dev/null || echo "")
  rm -f /tmp/gram_hook_response.json

  # Check if the API call was successful
  if [ "$HTTP_CODE" -ge 200 ] && [ "$HTTP_CODE" -lt 300 ]; then
    # Transform API response to Claude Code hook format
    output_success_json "$hook_event_name"
    exit 0
  fi

  # API error - use exit code 2 to block with error message
  echo "Gram API error (HTTP $HTTP_CODE)" >&2
  if [ -n "$RESPONSE" ]; then
    echo "$RESPONSE" >&2
  fi
  exit 2
}
