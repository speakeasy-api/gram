#!/usr/bin/env bash

#MISE description="Test the standalone MCP client and auth upstream mocks without the local stack"
#MISE dir="{{ config_root }}"

set -euo pipefail
exec node --test "$@" e2e/mcp-auth/*.test.ts
