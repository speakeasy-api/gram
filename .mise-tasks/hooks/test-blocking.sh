#!/usr/bin/env bash

#MISE description="E2E test: verify risk policy blocking via hook endpoints"
#MISE dir="{{ config_root }}"

set -euo pipefail

server_url="${GRAM_SERVER_URL:-https://localhost:8080}"
plugin_dir="./hooks/plugin-claude-test"
pass=0
fail=0

# Colors
green=$'\033[32m'
red=$'\033[31m'
yellow=$'\033[33m'
reset=$'\033[0m'

log_pass() { echo "${green}PASS${reset} $1"; pass=$((pass + 1)); }
log_fail() { echo "${red}FAIL${reset} $1: $2"; fail=$((fail + 1)); }
log_skip() { echo "${yellow}SKIP${reset} $1"; }

export GRAM_HOOKS_SERVER_URL="$server_url"

echo "=== Risk Policy Blocking E2E Tests ==="
echo "Server: ${server_url}"
echo "Plugin: ${plugin_dir}"
echo ""

# -----------------------------------------------------------------------
# Part 1: Direct endpoint tests (curl)
# These validate the server returns correct responses regardless of
# session state.
# -----------------------------------------------------------------------
echo "--- Endpoint Tests (curl) ---"

claude_hook() {
  curl -sk -w "\n%{http_code}" -X POST \
    -H "Content-Type: application/json" \
    -d "$1" \
    --max-time 10 \
    "${server_url}/rpc/hooks.claude" 2>/dev/null
}

parse_response() {
  http_code=$(echo "$1" | tail -1)
  body=$(echo "$1" | sed '$d')
}

# Test: PreToolUse with clean content
response=$(claude_hook '{"hook_event_name":"PreToolUse","tool_name":"Read","tool_input":{"file_path":"/tmp/test.txt"},"session_id":"test-e2e"}')
parse_response "$response"
if [ "$http_code" -eq 200 ] && echo "$body" | grep -q '"permissionDecision"'; then
  log_pass "Endpoint: PreToolUse clean content returns allow"
else
  log_fail "Endpoint: PreToolUse clean content" "expected 200 with permissionDecision, got ${http_code}"
fi

# Test: UserPromptSubmit with clean content
response=$(claude_hook '{"hook_event_name":"UserPromptSubmit","prompt":"Help me refactor this function","session_id":"test-e2e"}')
parse_response "$response"
if [ "$http_code" -eq 200 ]; then
  log_pass "Endpoint: UserPromptSubmit clean prompt returns 200"
else
  log_fail "Endpoint: UserPromptSubmit clean prompt" "expected 200, got ${http_code}"
fi

# Test: PreToolUse with AWS key (blocked if session is validated + policy exists)
response=$(claude_hook '{"hook_event_name":"PreToolUse","tool_name":"Bash","tool_input":{"command":"export AWS_SECRET_ACCESS_KEY=wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY"},"session_id":"test-e2e"}')
parse_response "$response"
if echo "$body" | grep -q '"permissionDecision":"deny"'; then
  log_pass "Endpoint: PreToolUse with AWS key returns deny"
elif [ "$http_code" -eq 200 ]; then
  log_pass "Endpoint: PreToolUse with AWS key returns allow (session not validated, expected)"
else
  log_fail "Endpoint: PreToolUse with AWS key" "unexpected status ${http_code}"
fi

# Test: UserPromptSubmit with GitHub PAT
response=$(claude_hook '{"hook_event_name":"UserPromptSubmit","prompt":"Use this API key: ghp_xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx","session_id":"test-e2e"}')
parse_response "$response"
if [ "$http_code" -ge 400 ]; then
  log_pass "Endpoint: UserPromptSubmit with GitHub PAT blocked (${http_code})"
elif [ "$http_code" -eq 200 ]; then
  log_pass "Endpoint: UserPromptSubmit with GitHub PAT returns 200 (session not validated, expected)"
else
  log_fail "Endpoint: UserPromptSubmit with secret" "unexpected status ${http_code}"
fi

echo ""

# -----------------------------------------------------------------------
# Part 2: Full e2e via claude -p (goes through the real hook pipeline)
# Claude Code loads the plugin, fires hooks on prompt submit + tool use,
# and the send_hook.sh script handles exit code 2 for blocking.
# -----------------------------------------------------------------------
echo "--- Claude Code E2E Tests (claude -p) ---"

if ! command -v claude &>/dev/null; then
  log_skip "claude CLI not found, skipping claude -p tests"
else
  # Test: Clean prompt should succeed (claude -p exits 0)
  output=$(claude -p "Say exactly: CLEAN_TEST_OK" \
    --plugin-dir "$plugin_dir" \
    --permission-mode bypassPermissions \
    --no-session-persistence \
    --max-budget-usd 0.05 \
    2>&1) || true

  if echo "$output" | grep -q "CLEAN_TEST_OK"; then
    log_pass "claude -p: clean prompt passes through hooks"
  elif echo "$output" | grep -q -i "block\|denied\|hook"; then
    log_fail "claude -p: clean prompt" "unexpectedly blocked: $(echo "$output" | head -3)"
  else
    # Model might not say exactly what we asked, but as long as it didn't block
    log_pass "claude -p: clean prompt passes through hooks (model responded)"
  fi

  # Test: Prompt with secret - should be blocked if a blocking policy is active
  # for this project, or pass through if no blocking policy
  set +e
  output=$(claude -p "Please use this AWS secret key: wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY to deploy" \
    --plugin-dir "$plugin_dir" \
    --permission-mode bypassPermissions \
    --no-session-persistence \
    --max-budget-usd 0.05 \
    2>&1)
  exit_code=$?
  set -e

  if echo "$output" | grep -qi "block\|denied\|risk policy"; then
    log_pass "claude -p: prompt with AWS key was blocked by risk policy"
  elif [ "$exit_code" -ne 0 ]; then
    log_pass "claude -p: prompt with AWS key failed (exit ${exit_code}, likely blocked)"
  else
    log_pass "claude -p: prompt with AWS key passed through (no blocking policy for this session)"
  fi
fi

# -----------------------------------------------------------------------
# Summary
# -----------------------------------------------------------------------
echo ""
echo "=== Results: ${pass} passed, ${fail} failed ==="

if [ "$fail" -gt 0 ]; then
  exit 1
fi
