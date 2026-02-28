#!/usr/bin/env bash
# Forward PostToolUse hook events to the Gram server.
# Reads the hook payload from stdin and POSTs it to the hooks service.

# Debug logging
echo "[$(date)] PostToolUse hook called" >> /tmp/gram-hooks-debug.log

# Load user configuration
GRAM_CONFIG_FILE="$HOME/.gram/config"
if [ -f "$GRAM_CONFIG_FILE" ]; then
  source "$GRAM_CONFIG_FILE"
fi

# Load common functions from plugin directory
# GRAM_PLUGIN_ROOT should be set by the environment
if [ -z "$GRAM_PLUGIN_ROOT" ]; then
  echo "ERROR: GRAM_PLUGIN_ROOT not set" >> /tmp/gram-hooks-debug.log
  exit 1
fi

source "${GRAM_PLUGIN_ROOT}/scripts/common.sh"

# Validate environment variables
validate_env_vars "PostToolUse"

# Call Gram API
call_gram_api "postToolUse" "PostToolUse"
