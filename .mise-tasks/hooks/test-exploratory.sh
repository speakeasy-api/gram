#!/usr/bin/env bash

#MISE description="Exploratory test: block then flag a secret through the full claude -p hook pipeline"
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

# The claude -p plugin authenticates via OTEL logs, which maps the session
# to a project. Policies are scoped to that project. We update ALL policies
# in the DB to ensure the test works regardless of which project the
# session resolves to.

claude_with_plugin() {
  claude -p "$1" \
    --plugin-dir ./hooks/plugin-claude-test \
    --permission-mode bypassPermissions \
    --max-budget-usd 0.10 2>&1
}

# Warmup: run a clean prompt first to establish a session and let OTEL
# logs validate it in Redis. The blocking hooks need session metadata
# to resolve the project.
echo "--- Warmup: establish session ---"
set +e
claude -p "hello" \
  --plugin-dir ./hooks/plugin-claude-test \
  --permission-mode bypassPermissions \
  --max-budget-usd 0.05 > /dev/null 2>&1
set -e
sleep 3
echo "Session warmup complete"
echo ""

echo "--- Step 1: Set ALL policies to 'block' ---"
docker exec gram-gram-db-1 psql -U gram -d gram -c \
  "UPDATE risk_policies SET action = 'block' WHERE deleted IS FALSE;"
echo ""

echo "--- Step 2: Send AWS key via claude -p (expect blocked) ---"
set +e
output=$(claude_with_plugin "hey AKIAIOSFODNN7EXAMPLE")
exit_code=$?
set -e
echo "Exit code: $exit_code"
echo "Output: '$output'"
echo ""
if [ -z "$output" ] || echo "$output" | grep -qi "block\|denied\|hook"; then
  echo "PASS: Prompt with secret was BLOCKED (empty output)"
else
  echo "WARN: Got output despite block policy. Session may not have validated in time."
  echo "      This is expected on the first run if OTEL logs haven't been sent yet."
fi
echo ""

echo "--- Step 3: Send clean prompt via claude -p (expect allowed) ---"
set +e
output=$(claude_with_plugin "say exactly: CLEAN_TEST_OK")
exit_code=$?
set -e
echo "Exit code: $exit_code"
echo "Output: '$output'"
echo ""
if [ -n "$output" ]; then
  echo "PASS: Clean prompt was ALLOWED (model responded)"
else
  echo "INFO: Empty output (may have been rate limited or budget exceeded)"
fi
echo ""

echo "--- Step 4: Set ALL policies to 'flag' ---"
docker exec gram-gram-db-1 psql -U gram -d gram -c \
  "UPDATE risk_policies SET action = 'flag' WHERE deleted IS FALSE;"
echo ""

echo "--- Step 5: Send same AWS key via claude -p (expect allowed, flag only) ---"
set +e
output=$(claude_with_plugin "hey AKIAIOSFODNN7EXAMPLE")
exit_code=$?
set -e
echo "Exit code: $exit_code"
echo "Output: '$output'"
echo ""
if [ -n "$output" ]; then
  echo "PASS: Prompt with secret was ALLOWED (flag policy, model responded)"
else
  echo "INFO: Empty output (budget may be exhausted from previous steps)"
fi

echo ""
echo "=== Done ==="
echo "Full log saved to: $log"
