#!/usr/bin/env bash
# Forward PreToolUse hook events to the Gram server.
# Reads the hook payload from stdin and POSTs it to the hooks service.

# Load user configuration
GRAM_CONFIG_FILE="$HOME/.gram/config"
if [ -f "$GRAM_CONFIG_FILE" ]; then
  source "$GRAM_CONFIG_FILE"
fi

# Load common functions from plugin directory
# GRAM_PLUGIN_ROOT should be set by the environment
if [ -z "$GRAM_PLUGIN_ROOT" ]; then
  echo "ERROR: GRAM_PLUGIN_ROOT not set" >&2
  exit 1
fi

source "${GRAM_PLUGIN_ROOT}/scripts/common.sh"

# Validate environment variables
validate_env_vars "PreToolUse"

# Call Gram API
call_gram_api "preToolUse" "PreToolUse"
