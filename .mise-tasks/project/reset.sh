#!/usr/bin/env bash

#MISE description="Reset project-home journey resources in the local database"
#MISE dir="{{ config_root }}"
#MISE confirm="Reset project-home journey resources in the local database?"

#USAGE flag "--project <slug>" help="Project slug to reset" default="default"
#USAGE flag "--project-id <id>" help="Exact project ID to reset when the slug is ambiguous" default=""
#USAGE flag "--policy-id <id>" help="Exact risk policy ID to remove" default=""

set -euo pipefail

project_slug="${usage_project:-default}"
project_id="${usage_project_id:-}"
policy_id="${usage_policy_id:-}"

if [ -z "$project_slug" ]; then
  echo "project:reset: --project must not be empty" >&2
  exit 2
fi

docker exec -i "${COMPOSE_PROJECT_NAME:-gram}-gram-db-1" \
  psql -U "$DB_USER" -d "$DB_NAME" -v ON_ERROR_STOP=1 -v project_slug="$project_slug" -v project_id="$project_id" -v policy_id="$policy_id" <<'SQL'
BEGIN;

CREATE TEMP TABLE reset_project ON COMMIT DROP AS
SELECT id
FROM projects
WHERE deleted IS FALSE
  AND (
    (NULLIF(:'project_id', '') IS NOT NULL AND id = NULLIF(:'project_id', '')::uuid)
    OR (NULLIF(:'project_id', '') IS NULL AND slug = :'project_slug')
  );

DO $$
BEGIN
  IF (SELECT count(*) FROM reset_project) = 0 THEN
    RAISE EXCEPTION 'active project not found';
  END IF;
  IF (SELECT count(*) FROM reset_project) > 1 THEN
    RAISE EXCEPTION 'project slug is ambiguous; pass --project-id';
  END IF;
END
$$;

SELECT format('Resetting project %s (%s)', :'project_slug', id)
FROM reset_project;

-- Journey A names every catalog install with the _Governed suffix. Restrict
-- this reset to those remote/unproxied servers so the seeded tunneled MCP
-- server and unrelated project resources survive.
CREATE TEMP TABLE journey_mcp_servers ON COMMIT DROP AS
SELECT
  s.id,
  s.remote_mcp_server_id,
  s.unproxied_mcp_server_id,
  s.user_session_issuer_id
FROM mcp_servers AS s
JOIN reset_project AS p ON p.id = s.project_id
WHERE s.deleted IS FALSE
  AND right(s.name, 9) = '_Governed'
  AND (s.remote_mcp_server_id IS NOT NULL OR s.unproxied_mcp_server_id IS NOT NULL);

CREATE TEMP TABLE journey_default_issuers ON COMMIT DROP AS
SELECT DISTINCT s.user_session_issuer_id AS id
FROM journey_mcp_servers AS s
JOIN user_session_issuers AS issuer ON issuer.id = s.user_session_issuer_id
WHERE s.user_session_issuer_id IS NOT NULL
  AND issuer.classification = 'project_default_idp'
  AND issuer.deleted IS FALSE
  AND NOT EXISTS (
    SELECT 1
    FROM mcp_servers AS other
    WHERE other.user_session_issuer_id = s.user_session_issuer_id
      AND other.deleted IS FALSE
      AND NOT EXISTS (
        SELECT 1
        FROM journey_mcp_servers AS selected
        WHERE selected.id = other.id
      )
  )
  AND NOT EXISTS (
    SELECT 1 FROM meta_mcp_servers AS other
    WHERE other.user_session_issuer_id = s.user_session_issuer_id
      AND other.deleted IS FALSE
  )
  AND NOT EXISTS (
    SELECT 1 FROM toolsets AS other
    WHERE other.user_session_issuer_id = s.user_session_issuer_id
      AND other.deleted IS FALSE
  );

CREATE TEMP TABLE journey_default_plugins ON COMMIT DROP AS
SELECT DISTINCT ps.plugin_id AS id
FROM plugin_servers AS ps
JOIN journey_mcp_servers AS s ON s.id = ps.mcp_server_id
JOIN plugins AS p ON p.id = ps.plugin_id
WHERE ps.deleted IS FALSE
  AND p.is_default IS TRUE
  AND p.deleted IS FALSE;

CREATE TEMP TABLE journey_remote_sources ON COMMIT DROP AS
SELECT DISTINCT s.remote_mcp_server_id AS id
FROM journey_mcp_servers AS s
WHERE s.remote_mcp_server_id IS NOT NULL
  AND NOT EXISTS (
    SELECT 1
    FROM mcp_servers AS other
    WHERE other.remote_mcp_server_id = s.remote_mcp_server_id
      AND other.deleted IS FALSE
      AND NOT EXISTS (
        SELECT 1
        FROM journey_mcp_servers AS selected
        WHERE selected.id = other.id
      )
  );

CREATE TEMP TABLE journey_unproxied_sources ON COMMIT DROP AS
SELECT DISTINCT s.unproxied_mcp_server_id AS id
FROM journey_mcp_servers AS s
WHERE s.unproxied_mcp_server_id IS NOT NULL
  AND NOT EXISTS (
    SELECT 1
    FROM mcp_servers AS other
    WHERE other.unproxied_mcp_server_id = s.unproxied_mcp_server_id
      AND other.deleted IS FALSE
      AND NOT EXISTS (
        SELECT 1
        FROM journey_mcp_servers AS selected
        WHERE selected.id = other.id
      )
  );

\echo 'Removing MCP child resources...'
DELETE FROM assistant_mcp_servers
WHERE mcp_server_id IN (SELECT id FROM journey_mcp_servers);

DELETE FROM mcp_metadata
WHERE mcp_server_id IN (SELECT id FROM journey_mcp_servers);

UPDATE mcp_server_tool_metadata
SET deleted_at = clock_timestamp(), updated_at = clock_timestamp()
WHERE mcp_server_id IN (SELECT id FROM journey_mcp_servers)
  AND deleted IS FALSE;

UPDATE mcp_endpoints
SET deleted_at = clock_timestamp(), updated_at = clock_timestamp()
WHERE mcp_server_id IN (SELECT id FROM journey_mcp_servers)
  AND deleted IS FALSE;

UPDATE meta_mcp_server_members
SET deleted_at = clock_timestamp(), updated_at = clock_timestamp()
WHERE mcp_server_id IN (SELECT id FROM journey_mcp_servers)
  AND deleted IS FALSE;

UPDATE organization_mcp_collection_server_attachments
SET deleted_at = clock_timestamp(), updated_at = clock_timestamp()
WHERE mcp_server_id IN (SELECT id FROM journey_mcp_servers)
  AND deleted IS FALSE;

UPDATE plugin_servers
SET deleted_at = clock_timestamp(), updated_at = clock_timestamp()
WHERE mcp_server_id IN (SELECT id FROM journey_mcp_servers)
  AND deleted IS FALSE;

UPDATE mcp_servers
SET deleted_at = clock_timestamp(), updated_at = clock_timestamp()
WHERE id IN (SELECT id FROM journey_mcp_servers);

\echo 'Removing MCP sources...'
-- Preserve a source or issuer if another active MCP server still references it.
UPDATE remote_mcp_server_headers
SET deleted_at = clock_timestamp(), updated_at = clock_timestamp()
WHERE remote_mcp_server_id IN (SELECT id FROM journey_remote_sources)
  AND deleted IS FALSE;

UPDATE remote_mcp_servers
SET deleted_at = clock_timestamp(), updated_at = clock_timestamp()
WHERE id IN (SELECT id FROM journey_remote_sources)
  AND deleted IS FALSE;

UPDATE unproxied_mcp_servers
SET deleted_at = clock_timestamp(), updated_at = clock_timestamp()
WHERE id IN (SELECT id FROM journey_unproxied_sources)
  AND deleted IS FALSE;

\echo 'Removing orphaned journey session state...'
DELETE FROM remote_session_client_user_session_issuers
WHERE user_session_issuer_id IN (SELECT id FROM journey_default_issuers);

UPDATE user_session_consents
SET deleted_at = clock_timestamp(), updated_at = clock_timestamp()
WHERE user_session_client_id IN (
  SELECT id FROM user_session_clients
  WHERE user_session_issuer_id IN (SELECT id FROM journey_default_issuers)
)
  AND deleted IS FALSE;

UPDATE user_sessions
SET deleted_at = clock_timestamp(), updated_at = clock_timestamp()
WHERE user_session_issuer_id IN (SELECT id FROM journey_default_issuers)
  AND deleted IS FALSE;

UPDATE user_session_clients
SET deleted_at = clock_timestamp(), updated_at = clock_timestamp()
WHERE user_session_issuer_id IN (SELECT id FROM journey_default_issuers)
  AND deleted IS FALSE;

UPDATE user_session_issuer_cimd_clients
SET deleted_at = clock_timestamp(), updated_at = clock_timestamp()
WHERE user_session_issuer_id IN (SELECT id FROM journey_default_issuers)
  AND deleted IS FALSE;

UPDATE user_session_issuers
SET deleted_at = clock_timestamp(), updated_at = clock_timestamp()
WHERE id IN (SELECT id FROM journey_default_issuers)
  AND deleted IS FALSE;

\echo 'Removing empty journey default plugins...'
CREATE TEMP TABLE empty_default_plugins ON COMMIT DROP AS
SELECT p.id
FROM plugins AS p
JOIN journey_default_plugins AS journey ON journey.id = p.id
WHERE p.deleted IS FALSE
  AND NOT EXISTS (
    SELECT 1
    FROM plugin_servers AS ps
    WHERE ps.plugin_id = p.id
      AND ps.deleted IS FALSE
  );

DELETE FROM plugin_assignments
WHERE plugin_id IN (SELECT id FROM empty_default_plugins);

UPDATE skill_distributions
SET revoked_at = clock_timestamp(), updated_at = clock_timestamp()
WHERE plugin_id IN (SELECT id FROM empty_default_plugins)
  AND revoked_at IS NULL;

UPDATE plugins
SET deleted_at = clock_timestamp(), updated_at = clock_timestamp()
WHERE id IN (SELECT id FROM empty_default_plugins)
  AND deleted IS FALSE;

CREATE TEMP TABLE journey_risk_policies ON COMMIT DROP AS
SELECT policy.id
FROM risk_policies AS policy
JOIN reset_project AS rp ON rp.id = policy.project_id
WHERE NULLIF(:'policy_id', '') IS NOT NULL
  AND policy.id = NULLIF(:'policy_id', '')::uuid
  AND policy.deleted IS FALSE;

DO $$
BEGIN
  IF NULLIF(:'policy_id', '') IS NOT NULL
     AND NOT EXISTS (SELECT 1 FROM journey_risk_policies) THEN
    RAISE EXCEPTION 'risk policy not found in the selected project';
  END IF;
END
$$;

\echo 'Removing the project-home blocking secrets policy...'
UPDATE tool_call_blocks
SET deleted_at = clock_timestamp(), updated_at = clock_timestamp()
WHERE risk_policy_id IN (SELECT id FROM journey_risk_policies)
  AND deleted IS FALSE;

DELETE FROM risk_results
WHERE risk_policy_id IN (SELECT id FROM journey_risk_policies);

DELETE FROM risk_exclusions
WHERE risk_policy_id IN (SELECT id FROM journey_risk_policies);

UPDATE risk_policy_bypass_requests
SET deleted_at = clock_timestamp(), updated_at = clock_timestamp()
WHERE risk_policy_id IN (SELECT id FROM journey_risk_policies)
  AND deleted IS FALSE;

UPDATE risk_policies
SET deleted_at = clock_timestamp(), updated_at = clock_timestamp()
WHERE id IN (SELECT id FROM journey_risk_policies)
  AND deleted IS FALSE;

COMMIT;
SQL

echo "Project-home journey reset complete for project: $project_slug"
