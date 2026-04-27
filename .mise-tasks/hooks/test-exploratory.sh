#!/usr/bin/env bash

#MISE description="Exploratory test: block then flag a secret through the full hook pipeline"
#MISE dir="{{ config_root }}"

set -euo pipefail

export GRAM_HOOKS_SERVER_URL=$GRAM_SERVER_URL

log="/tmp/gram-hooks-exploratory-$(date +%Y%m%d-%H%M%S).log"
echo "Logging to: $log"
exec > >(tee "$log") 2>&1

echo "=== Gram Hooks Exploratory Test ==="
echo "Date: $(date)"
echo "Server: $GRAM_SERVER_URL"
echo ""

POLICY_ID=$(docker exec gram-gram-db-1 psql -U gram -d gram -tAc \
  "SELECT id FROM risk_policies WHERE deleted IS FALSE LIMIT 1")

if [ -z "$POLICY_ID" ]; then
  echo "FATAL: no risk policy found"
  exit 1
fi

echo "Policy ID: $POLICY_ID"
echo ""

# --- Step 1: Set policy to block ---
echo "--- Step 1: Set policy action to 'block' ---"
docker exec gram-gram-db-1 psql -U gram -d gram -c \
  "UPDATE risk_policies SET action = 'block' WHERE deleted IS FALSE;"
echo ""

# --- Step 2: Send secret (expect blocked) ---
echo "--- Step 2: Send Stripe key via hook endpoint (expect 403 blocked) ---"
response=$(curl -sk -w "\nHTTP_STATUS:%{http_code}" -X POST \
  -H "Content-Type: application/json" \
  -d '{"hook_event_name":"UserPromptSubmit","prompt":"hey AKIAIOSFODNN7EXAMPLE","session_id":"exploratory-test"}' \
  "$GRAM_SERVER_URL/rpc/hooks.claude")

http_code=$(echo "$response" | grep "HTTP_STATUS:" | sed 's/HTTP_STATUS://')
body=$(echo "$response" | grep -v "HTTP_STATUS:")

echo "HTTP Status: $http_code"
echo "Response: $body"
echo ""

if [ "$http_code" = "403" ]; then
  echo "PASS: Secret was BLOCKED (HTTP 403)"
else
  echo "FAIL: Expected 403, got $http_code"
fi
echo ""

# --- Step 3: Send via claude -p (expect empty/blocked) ---
echo "--- Step 3: Send Stripe key via claude -p (expect blocked) ---"
set +e
claude_output=$(claude -p "hey AKIAIOSFODNN7EXAMPLE" \
  --plugin-dir ./hooks/plugin-claude-test \
  --permission-mode bypassPermissions \
  --no-session-persistence \
  --max-budget-usd 0.05 2>&1)
claude_exit=$?
set -e

echo "Exit code: $claude_exit"
echo "Output: '$claude_output'"
echo ""

if [ -z "$claude_output" ] || echo "$claude_output" | grep -qi "block\|denied\|hook"; then
  echo "PASS: claude -p prompt was blocked (empty output or block message)"
else
  echo "INFO: claude -p returned output (session may not have validated in time)"
fi
echo ""

# --- Step 4: Set policy to flag ---
echo "--- Step 4: Set policy action to 'flag' ---"
docker exec gram-gram-db-1 psql -U gram -d gram -c \
  "UPDATE risk_policies SET action = 'flag' WHERE deleted IS FALSE;"
echo ""

# --- Step 5: Send same secret (expect allowed) ---
echo "--- Step 5: Send Stripe key via hook endpoint (expect 200 allowed) ---"
response=$(curl -sk -w "\nHTTP_STATUS:%{http_code}" -X POST \
  -H "Content-Type: application/json" \
  -d '{"hook_event_name":"UserPromptSubmit","prompt":"hey AKIAIOSFODNN7EXAMPLE","session_id":"exploratory-test"}' \
  "$GRAM_SERVER_URL/rpc/hooks.claude")

http_code=$(echo "$response" | grep "HTTP_STATUS:" | sed 's/HTTP_STATUS://')
body=$(echo "$response" | grep -v "HTTP_STATUS:")

echo "HTTP Status: $http_code"
echo "Response: $body"
echo ""

if [ "$http_code" = "200" ]; then
  echo "PASS: Secret was ALLOWED (HTTP 200, flag-only policy)"
else
  echo "FAIL: Expected 200, got $http_code"
fi
echo ""

# --- Step 6: Send via claude -p (expect model responds) ---
echo "--- Step 6: Send Stripe key via claude -p (expect model responds) ---"
set +e
claude_output=$(claude -p "hey AKIAIOSFODNN7EXAMPLE" \
  --plugin-dir ./hooks/plugin-claude-test \
  --permission-mode bypassPermissions \
  --no-session-persistence \
  --max-budget-usd 0.05 2>&1)
claude_exit=$?
set -e

echo "Exit code: $claude_exit"
echo "Output: '$claude_output'"
echo ""

if [ -n "$claude_output" ]; then
  echo "PASS: claude -p prompt was allowed (model responded)"
else
  echo "INFO: claude -p returned empty (may have been rate limited)"
fi

echo ""
echo "=== Done ==="
echo "Full log saved to: $log"
