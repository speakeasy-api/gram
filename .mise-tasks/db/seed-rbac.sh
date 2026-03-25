#!/usr/bin/env bash

#MISE description="Seed default RBAC grants for an organization (or all orgs)"
#USAGE arg "[org_id]" help="Organization ID to seed (omit to seed all orgs)"
#USAGE flag "--dry-run" help="Print the SQL without executing"

set -eo pipefail

ORG_ID="${usage_org_id:-}"
DRY_RUN="${usage_dry_run:-false}"

# Default grants for system roles.
# admin gets full access; member gets read + connect.
read -r -d '' GRANTS_SQL <<'SQL' || true
INSERT INTO principal_grants (organization_id, principal_urn, scope, resource)
SELECT org_id, principal_urn, scope, resource
FROM (
  SELECT unnest(:'org_ids'::text[]) AS org_id
) orgs
CROSS JOIN (
  VALUES
    ('role:admin', 'org:read',    '*'),
    ('role:admin', 'org:admin',   '*'),
    ('role:admin', 'build:read',  '*'),
    ('role:admin', 'build:write', '*'),
    ('role:admin', 'mcp:read',    '*'),
    ('role:admin', 'mcp:write',   '*'),
    ('role:admin', 'mcp:connect', '*'),
    ('role:member', 'org:read',    '*'),
    ('role:member', 'build:read',  '*'),
    ('role:member', 'mcp:read',    '*'),
    ('role:member', 'mcp:connect', '*')
) AS grants(principal_urn, scope, resource)
ON CONFLICT (organization_id, principal_urn, scope, resource) DO NOTHING;
SQL

run_psql() {
  if [[ -n "${DATABASE_URL:-}" ]]; then
    psql "${DATABASE_URL}" "$@"
  else
    docker compose exec -T gram-db psql -U "${DB_USER}" -d "${DB_NAME}" "$@"
  fi
}

if [[ -n "${ORG_ID}" ]]; then
  ORG_IDS_VALUE="{${ORG_ID}}"
  echo "Seeding default RBAC grants for org: ${ORG_ID}"
else
  ORG_IDS_VALUE=$(run_psql -Atc "SELECT '{' || string_agg(id, ',') || '}' FROM organization_metadata")
  if [[ -z "${ORG_IDS_VALUE}" ]]; then
    echo "No organizations found."
    exit 0
  fi
  ORG_COUNT=$(echo "${ORG_IDS_VALUE}" | tr ',' '\n' | wc -l | tr -d ' ')
  echo "Seeding default RBAC grants for ${ORG_COUNT} orgs"
fi

if [[ "${DRY_RUN}" == "true" ]]; then
  echo "--- DRY RUN (no changes will be made) ---"
  echo "${GRANTS_SQL}" | sed "s/:'org_ids'::text\[\]/'${ORG_IDS_VALUE}'::text[]/g"
  exit 0
fi

run_psql -v "org_ids=${ORG_IDS_VALUE}" -c "${GRANTS_SQL}"

echo "Done."
