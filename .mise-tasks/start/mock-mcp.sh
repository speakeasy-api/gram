#!/usr/bin/env bash

#MISE dir="{{ config_root }}/server"
#MISE description="Start the local mock MCP server the local seed registers as a remote MCP server"
#MISE hide=true

set -e

exec go run ./cmd/mock-mcp
