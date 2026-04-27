#!/usr/bin/env bash

#MISE description="Test the Gram hooks Claude plugin headlessly (no TTY required)"
#MISE dir="{{ config_root }}"

#USAGE flag "--local" help="Always use local plugin directory instead of published plugin"
#USAGE flag "-p --prompt <prompt>" help="Prompt to send to Claude" default="Say exactly: HOOKS_TEST_OK"

set -euo pipefail

export GRAM_HOOKS_SERVER_URL=$GRAM_SERVER_URL

plugin_args=()
if [ "${usage_local:-}" = "true" ] || ! git diff --quiet HEAD -- hooks/; then
  echo "Using local plugin directory: ./hooks/plugin-claude-test"
  plugin_args=(--plugin-dir ./hooks/plugin-claude-test)
else
  echo "No local changes in hooks/ — using published plugin"
fi

echo ""
exec claude -p "${usage_prompt}" \
  "${plugin_args[@]}" \
  --permission-mode bypassPermissions \
  --no-session-persistence \
  --max-budget-usd 0.05
