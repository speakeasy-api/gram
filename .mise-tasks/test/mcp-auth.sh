#!/usr/bin/env bash

#MISE description="Run dashboard-provisioned MCP auth end-to-end scenarios against the local stack"
#MISE dir="{{ config_root }}/e2e"

set -euo pipefail

# Target an already running local stack. Do not seed, mutate feature flags or
# bootstrap product records: the browser must perform every administration step.
exec aube exec playwright test --config mcp-auth.config.ts "$@"
