#!/usr/bin/env bash
#MISE description="Start the local MCP registry"

set -e

docker compose --profile local-registry up -d || exit 1

echo ""
echo "MCP registry started successfully!"
echo "Registry URL: ${LOCAL_MCP_REGISTRY_URL}"
