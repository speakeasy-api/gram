#!/usr/bin/env bash
# Forward PostToolUse hook events to the Gram server.
# Reads the hook payload from stdin and POSTs it to the hooks service.

# Check if GRAM_API_KEY is set
if [ -z "$GRAM_API_KEY" ]; then
  exit 0
fi

INPUT=$(cat)
curl -s -X POST http://localhost:8080/rpc/hooks.postToolUse \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer $GRAM_API_KEY" \
  -d "$INPUT" \
  > /dev/null 2>&1
exit 0
