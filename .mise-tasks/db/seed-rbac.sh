#!/usr/bin/env bash

#MISE description="Seed default RBAC grants for an organization"
#USAGE arg "<org_id>" help="The organization ID to seed grants for"

set -eo pipefail

ORG_ID="${usage_org_id:?Usage: mise run db:seed-rbac <org_id>}"

# Default grants for system roles.
# admin gets full access; member gets read + connect.
GRANTS_SQL=$(cat <<'SQL'
INSERT INTO principal_grants (organization_id, principal_urn, scope, resource)
VALUES
  ($1, 'role:admin', 'org:read',    '*'),
  ($1, 'role:admin', 'org:admin',   '*'),
  ($1, 'role:admin', 'build:read',  '*'),
  ($1, 'role:admin', 'build:write', '*'),
  ($1, 'role:admin', 'mcp:read',    '*'),
  ($1, 'role:admin', 'mcp:write',   '*'),
  ($1, 'role:admin', 'mcp:connect', '*'),
  ($1, 'role:member', 'org:read',    '*'),
  ($1, 'role:member', 'build:read',  '*'),
  ($1, 'role:member', 'mcp:read',    '*'),
  ($1, 'role:member', 'mcp:connect', '*')
ON CONFLICT (organization_id, principal_urn, scope, resource) DO NOTHING;
SQL
)

echo "Seeding default RBAC grants for org: ${ORG_ID}"

if [[ -n "${DATABASE_URL:-}" ]]; then
  psql "${DATABASE_URL}" -c "${GRANTS_SQL}" -v "1=${ORG_ID}"
else
  docker compose exec -T gram-db psql -U "${DB_USER}" -d "${DB_NAME}" -c "${GRANTS_SQL}" -v "1=${ORG_ID}"
fi

echo "Done."
