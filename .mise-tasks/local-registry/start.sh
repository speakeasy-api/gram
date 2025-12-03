#!/usr/bin/env bash
#MISE description="Start the local MCP registry"

set -e

docker compose -p gram-local-registry -f local-registry-compose.yaml up -d || exit 1

# Wait for the registry database to be ready
until docker compose -p gram-local-registry -f local-registry-compose.yaml exec mcp-registry-db psql -U mcp_registry -d mcp_registry -c "SELECT 1" > /dev/null 2>&1; do
    echo "Waiting for MCP registry database to be ready..."
    sleep 1
done

echo ""
echo "MCP registry started successfully!"
echo "Registry URL: ${LOCAL_MCP_REGISTRY_URL}"
