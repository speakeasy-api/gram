#!/usr/bin/env bash
#MISE description="Stop the local MCP registry"

set -e

docker compose -p gram-local-registry -f local-registry-compose.yaml down --remove-orphans
