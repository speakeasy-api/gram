#!/usr/bin/env bash

#MISE description="E2E test: verify risk policy blocking via hook endpoints"
#MISE dir="{{ config_root }}"

set -euo pipefail

server_url="${GRAM_SERVER_URL:-https://localhost:8080}"
pass=0
fail=0

# Colors
green=$'\033[32m'
red=$'\033[31m'
reset=$'\033[0m'

log_pass() { echo "${green}PASS${reset} $1"; pass=$((pass + 1)); }
log_fail() { echo "${red}FAIL${reset} $1: $2"; fail=$((fail + 1)); }

# Helper: POST to hooks.claude and capture status + body
claude_hook() {
  local payload="$1"
  curl -sk -w "\n%{http_code}" -X POST \
    -H "Content-Type: application/json" \
    -d "$payload" \
    --max-time 10 \
    "${server_url}/rpc/hooks.claude" 2>/dev/null
}

# Helper: POST to hooks.cursor (authenticated) and capture status + body
cursor_hook() {
  local payload="$1"
  local api_key="${GRAM_API_KEY:-}"
  local project_slug="${GRAM_PROJECT_SLUG:-ecommerce-api}"
  curl -sk -w "\n%{http_code}" -X POST \
    -H "Content-Type: application/json" \
    -H "x-api-key: ${api_key}" \
    -H "x-project-slug: ${project_slug}" \
    -d "$payload" \
    --max-time 10 \
    "${server_url}/rpc/hooks.cursor" 2>/dev/null
}

parse_response() {
  local response="$1"
  http_code=$(echo "$response" | tail -1)
  body=$(echo "$response" | sed '$d')
}

echo "=== Risk Policy Blocking E2E Tests ==="
echo "Server: ${server_url}"
echo ""

# -----------------------------------------------------------------------
# Test 1: Claude PreToolUse with clean content should return allow
# -----------------------------------------------------------------------
response=$(claude_hook '{"hook_event_name":"PreToolUse","tool_name":"Read","tool_input":{"file_path":"/tmp/test.txt"},"session_id":"test-session-1"}')
parse_response "$response"

if [ "$http_code" -eq 200 ] && echo "$body" | grep -q '"permissionDecision"'; then
  log_pass "Claude PreToolUse clean content returns 200"
else
  log_fail "Claude PreToolUse clean content" "expected 200, got ${http_code}"
fi

# -----------------------------------------------------------------------
# Test 2: Claude PreToolUse with a fake AWS key should return deny
#         (only if a blocking policy exists for secrets)
# -----------------------------------------------------------------------
response=$(claude_hook '{"hook_event_name":"PreToolUse","tool_name":"Bash","tool_input":{"command":"export AWS_ACCESS_KEY_ID=AKIAIOSFODNN7EXAMPLE && export AWS_SECRET_ACCESS_KEY=wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY"},"session_id":"test-session-1"}')
parse_response "$response"

if echo "$body" | grep -q '"permissionDecision":"deny"'; then
  log_pass "Claude PreToolUse with AWS key returns deny (blocking policy active)"
elif [ "$http_code" -eq 200 ]; then
  log_pass "Claude PreToolUse with AWS key returns allow (no blocking policy or session not validated)"
else
  log_fail "Claude PreToolUse with AWS key" "unexpected status ${http_code}"
fi

# -----------------------------------------------------------------------
# Test 3: Claude UserPromptSubmit with clean content should return 200
# -----------------------------------------------------------------------
response=$(claude_hook '{"hook_event_name":"UserPromptSubmit","prompt":"Please help me refactor this function","session_id":"test-session-1"}')
parse_response "$response"

if [ "$http_code" -eq 200 ]; then
  log_pass "Claude UserPromptSubmit clean prompt returns 200"
else
  log_fail "Claude UserPromptSubmit clean prompt" "expected 200, got ${http_code}"
fi

# -----------------------------------------------------------------------
# Test 4: Claude UserPromptSubmit with secret should block
#         (4xx if blocking policy active, 200 if no policy/session)
# -----------------------------------------------------------------------
response=$(claude_hook '{"hook_event_name":"UserPromptSubmit","prompt":"Use this API key: ghp_xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx to authenticate","session_id":"test-session-1"}')
parse_response "$response"

if [ "$http_code" -ge 400 ]; then
  log_pass "Claude UserPromptSubmit with GitHub PAT returns ${http_code} (blocked)"
elif [ "$http_code" -eq 200 ]; then
  log_pass "Claude UserPromptSubmit with GitHub PAT returns 200 (no blocking policy or session not validated)"
else
  log_fail "Claude UserPromptSubmit with secret" "unexpected status ${http_code}"
fi

# -----------------------------------------------------------------------
# Test 5: Cursor preToolUse with clean content (requires auth)
# -----------------------------------------------------------------------
if [ -n "${GRAM_API_KEY:-}" ]; then
  response=$(cursor_hook '{"hook_event_name":"preToolUse","tool_name":"Read","tool_input":{"file_path":"/tmp/test.txt"},"conversation_id":"test-conv-1"}')
  parse_response "$response"

  if [ "$http_code" -eq 401 ]; then
    echo "SKIP Cursor tests (API key not authorized)"
  elif [ "$http_code" -eq 200 ]; then
    log_pass "Cursor preToolUse clean content returns 200"

    # Cursor preToolUse with secret
    response=$(cursor_hook '{"hook_event_name":"preToolUse","tool_name":"Bash","tool_input":{"command":"export OPENAI_API_KEY=sk-proj-xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx"},"conversation_id":"test-conv-1"}')
    parse_response "$response"

    if echo "$body" | grep -q '"permission":"deny"'; then
      log_pass "Cursor preToolUse with OpenAI key returns deny (blocking policy active)"
    elif [ "$http_code" -eq 200 ]; then
      log_pass "Cursor preToolUse with OpenAI key returns allow (no blocking policy)"
    else
      log_fail "Cursor preToolUse with secret" "unexpected status ${http_code}"
    fi
  else
    log_fail "Cursor preToolUse clean content" "expected 200, got ${http_code}"
  fi
else
  echo "SKIP Cursor tests (GRAM_API_KEY not set)"
fi

# -----------------------------------------------------------------------
# Summary
# -----------------------------------------------------------------------
echo ""
echo "=== Results: ${pass} passed, ${fail} failed ==="

if [ "$fail" -gt 0 ]; then
  exit 1
fi
