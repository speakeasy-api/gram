#!/usr/bin/env bash
#MISE description="Add the local MCP registry to Gram's mcp_registries table"
#USAGE flag "--registry-url <url>" env="LOCAL_MCP_REGISTRY_URL" required=#true help="The URL of the local MCP registry"

set -eo pipefail

REGISTRY_URL="${usage_registry_url:?Error: --registry-url not provided}"

REGISTRY_NAME="Local MCP Registry"

echo "Adding local MCP registry to Gram database..."
echo "  Name: $REGISTRY_NAME"
echo "  URL:  $REGISTRY_URL"
echo ""

# Upsert: insert or update on conflict
# The unique index is on url WHERE deleted IS FALSE
docker compose exec -T gram-db psql -U "${DB_USER}" -d "${DB_NAME}" <<EOF
INSERT INTO mcp_registries (name, url)
VALUES ('$REGISTRY_NAME', '$REGISTRY_URL')
ON CONFLICT (url) WHERE deleted IS FALSE
DO UPDATE SET name = EXCLUDED.name, updated_at = clock_timestamp();
EOF

echo "Done! Local MCP registry is now enabled in Gram."
