#!/usr/bin/env bash

#MISE description="Bootstrap local EMA signing infrastructure using a fresh dashboard admin session"
#MISE dir="{{ config_root }}"

set -euo pipefail
export MCP_AUTH_CONFIG_ROOT="$(pwd -P)"
export MCP_AUTH_SERVER_CWD="$MCP_AUTH_CONFIG_ROOT/server"
exec node e2e/mcp-auth/bootstrap.ts
