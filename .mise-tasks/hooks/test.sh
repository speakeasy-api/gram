#!/usr/bin/env bash

#MISE description="Test the Gram hooks Claude plugin locally"
#MISE dir="{{ config_root }}"

set -euo pipefail

echo "Starting Claude Code with Gram hooks plugin..."
echo ""
echo "Plugin directory: ./hooks/plugin-claude-test"
echo ""



export GRAM_HOOKS_SERVER_URL=http://localhost:8080
exec claude --plugin-dir ./hooks/plugin-claude-test --debug
