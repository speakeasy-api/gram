#!/usr/bin/env bash
# Forward PostToolUse hook events to the Gram server.
# Reads the hook payload from stdin and POSTs it to the hooks service.

# Load common functions
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
source "$SCRIPT_DIR/common.sh"

# Validate environment variables
validate_env_vars "PostToolUse"

# Call Gram API
call_gram_api "postToolUse" "PostToolUse"
