#!/usr/bin/env bash

#MISE description="Test the Gram hooks Claude plugin locally"
#MISE dir="{{ config_root }}"

set -euo pipefail

export GRAM_HOOKS_SERVER_URL=$GRAM_SERVER_URL

producer_tests=(
  "hooks/shared-producer/producer-core.test.mjs"
  "hooks/shared-producer/upload.test.mjs"
  "hooks/shared-producer/cache.test.mjs"
)

existing_tests=()
for test_file in "${producer_tests[@]}"; do
  if [ -f "$test_file" ]; then
    existing_tests+=("$test_file")
  fi
done

if [ "${#existing_tests[@]}" -gt 0 ]; then
  echo "Running shared producer tests..."
  node --test "${existing_tests[@]}"
  echo ""
fi

if git diff --quiet HEAD -- hooks/; then
  echo "No local changes in hooks/ — using published plugin"
  echo ""
  exec claude --debug
else
  echo "Local changes detected in hooks/ — using local plugin directory"
  echo "Plugin directory: ./hooks/plugin-claude-test"
  echo ""
  exec claude --plugin-dir ./hooks/plugin-claude-test --debug
fi
