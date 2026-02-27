#!/usr/bin/env bash
# Forward PreToolUse hook events to the Gram server.
# Reads the hook payload from stdin and POSTs it to the hooks service.

# Load common functions
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
source "$SCRIPT_DIR/common.sh"

# Validate environment variables
validate_env_vars "PreToolUse"

# Call Gram API
call_gram_api "preToolUse" "PreToolUse"
