#!/usr/bin/env bash

#MISE description="Exploratory test: block then flag a secret through the hook pipeline"
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

# Pre-seed a session in Redis so the hook handler can resolve the project.
# Uses the seed-session Go tool which writes msgpack (matching go-redis/cache format).
PROJECT_ID=$(docker exec gram-gram-db-1 psql -U gram -d gram -tAc \
  "SELECT id FROM projects LIMIT 1")
ORG_ID=$(docker exec gram-gram-db-1 psql -U gram -d gram -tAc \
  "SELECT organization_id FROM projects LIMIT 1")
SESSION_ID="test-exploratory-$$"

echo "Project: $PROJECT_ID"
echo "Org:     $ORG_ID"
echo "Session: $SESSION_ID"
echo ""

go run ./server/cmd/seed-session \
  --session-id="$SESSION_ID" \
  --project-id="$PROJECT_ID" \
  --org-id="$ORG_ID"

hook() {
  curl -sk -w "\n%{http_code}" -X POST \
    -H "Content-Type: application/json" \
    -d "{\"hook_event_name\":\"$1\",\"prompt\":\"$2\",\"session_id\":\"$SESSION_ID\",\"tool_name\":\"$3\",\"tool_input\":$4}" \
    "$GRAM_SERVER_URL/rpc/hooks.claude" 2>/dev/null
}

parse() {
  http_code=$(echo "$1" | tail -1)
  body=$(echo "$1" | sed '$d')
}

pass=0; fail=0

check() {
  local label="$1" expected="$2" actual="$3"
  if [ "$expected" = "$actual" ]; then
    echo "PASS $label (HTTP $actual)"
    pass=$((pass + 1))
  else
    echo "FAIL $label (expected $expected, got $actual)"
    echo "  Body: $body"
    fail=$((fail + 1))
  fi
}

# --- Block mode ---
echo "--- Set ALL policies to 'block' ---"
docker exec gram-gram-db-1 psql -U gram -d gram -tAc \
  "UPDATE risk_policies SET action = 'block' WHERE deleted IS FALSE;"
echo ""

response=$(hook "UserPromptSubmit" "hey AKIAIOSFODNN7REALKEY" "" "null")
parse "$response"
check "BLOCK UserPromptSubmit with AWS key" "403" "$http_code"

response=$(hook "PreToolUse" "" "Bash" "{\"command\":\"export AWS_SECRET_ACCESS_KEY=wJalrXUtnFEMI/K7MDENG/bPxRfiCYREALKEYXX\"}")
parse "$response"
if echo "$body" | grep -q '"permissionDecision":"deny"'; then
  echo "PASS BLOCK PreToolUse with AWS secret (deny)"
  pass=$((pass + 1))
else
  check "BLOCK PreToolUse with AWS secret" "200+deny" "$http_code"
fi

response=$(hook "UserPromptSubmit" "help me refactor this function" "" "null")
parse "$response"
check "BLOCK UserPromptSubmit clean prompt" "200" "$http_code"

echo ""

# --- Flag mode ---
echo "--- Set ALL policies to 'flag' ---"
docker exec gram-gram-db-1 psql -U gram -d gram -tAc \
  "UPDATE risk_policies SET action = 'flag' WHERE deleted IS FALSE;"
echo ""

response=$(hook "UserPromptSubmit" "hey AKIAIOSFODNN7REALKEY" "" "null")
parse "$response"
check "FLAG UserPromptSubmit with AWS key (should allow)" "200" "$http_code"

response=$(hook "PreToolUse" "" "Bash" "{\"command\":\"export AWS_SECRET_ACCESS_KEY=wJalrXUtnFEMI/K7MDENG/bPxRfiCYREALKEYXX\"}")
parse "$response"
if echo "$body" | grep -q '"permissionDecision":"allow"'; then
  echo "PASS FLAG PreToolUse with AWS secret (allow)"
  pass=$((pass + 1))
else
  check "FLAG PreToolUse with AWS secret" "200+allow" "$http_code"
fi

echo ""
echo "=== Results: $pass passed, $fail failed ==="
echo "Full log saved to: $log"

if [ "$fail" -gt 0 ]; then exit 1; fi
