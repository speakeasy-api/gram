#!/usr/bin/env bash

#MISE description="Serve standalone synthetic OAuth and MCP upstreams for dashboard-driven auth fixtures"
#MISE dir="{{ config_root }}"

set -euo pipefail
exec node e2e/mcp-auth/serve.ts "$@"
