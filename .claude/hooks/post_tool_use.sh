#!/usr/bin/env bash
# Forward PostToolUse hook events to the local Gram server.
# Reads the hook payload from stdin and POSTs it to the hooks service.
INPUT=$(cat)
curl -s -X POST http://localhost:8080/rpc/hooks.postToolUse \
  -H "Content-Type: application/json" \
  -d "$INPUT" \
  > /dev/null 2>&1
exit 0
