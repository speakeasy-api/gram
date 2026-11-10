#!/usr/bin/env bash

#MISE description="Start the throwaway local MCP fixture server (OAuth, shared token, demo API stub)"
#MISE hide=true

set -e

exec go run ./mcp-fixture/main
