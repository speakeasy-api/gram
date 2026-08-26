#!/usr/bin/env bash

#MISE description="Reset project-home journey resources in the local database"
#MISE dir="{{ config_root }}"
#MISE confirm="Reset project-home journey resources in the local database?"

#USAGE flag "--project <slug>" help="Project slug to reset" default="default"

set -euo pipefail

project_slug="${usage_project:-default}"

if [ -z "$project_slug" ]; then
  echo "project:reset: --project must not be empty" >&2
  exit 2
fi

docker exec -i "${COMPOSE_PROJECT_NAME:-gram}-gram-db-1" \
  psql -U "$DB_USER" -d "$DB_NAME" -v ON_ERROR_STOP=1 -v project_slug="$project_slug" <<'SQL'
BEGIN;

CREATE TEMP TABLE reset_project ON COMMIT DROP AS
SELECT id
FROM projects
WHERE slug = :'project_slug'
  AND deleted IS FALSE
ORDER BY created_at, id
LIMIT 1;

DO $$
BEGIN
  IF NOT EXISTS (SELECT 1 FROM reset_project) THEN
    RAISE EXCEPTION 'active project not found';
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

\echo 'Removing the project-home blocking secrets policy...'
DELETE FROM risk_results
WHERE risk_policy_id IN (
  SELECT policy.id
  FROM risk_policies AS policy
  JOIN reset_project AS rp ON rp.id = policy.project_id
  WHERE policy.deleted IS FALSE
    AND policy.policy_type = 'standard'
    AND policy.action = 'block'
    AND policy.audience_type = 'everyone'
    AND policy.auto_name IS TRUE
    AND policy.sources = ARRAY['gitleaks']::text[]
    AND policy.message_types @> ARRAY['tool_request', 'tool_response']::text[]
    AND cardinality(policy.message_types) = 2
);

DELETE FROM risk_exclusions
WHERE risk_policy_id IN (
  SELECT policy.id
  FROM risk_policies AS policy
  JOIN reset_project AS rp ON rp.id = policy.project_id
  WHERE policy.deleted IS FALSE
    AND policy.policy_type = 'standard'
    AND policy.action = 'block'
    AND policy.audience_type = 'everyone'
    AND policy.auto_name IS TRUE
    AND policy.sources = ARRAY['gitleaks']::text[]
    AND policy.message_types @> ARRAY['tool_request', 'tool_response']::text[]
    AND cardinality(policy.message_types) = 2
);

UPDATE risk_policy_bypass_requests
SET deleted_at = clock_timestamp(), updated_at = clock_timestamp()
WHERE risk_policy_id IN (
  SELECT policy.id
  FROM risk_policies AS policy
  JOIN reset_project AS rp ON rp.id = policy.project_id
  WHERE policy.deleted IS FALSE
    AND policy.policy_type = 'standard'
    AND policy.action = 'block'
    AND policy.audience_type = 'everyone'
    AND policy.auto_name IS TRUE
    AND policy.sources = ARRAY['gitleaks']::text[]
    AND policy.message_types @> ARRAY['tool_request', 'tool_response']::text[]
    AND cardinality(policy.message_types) = 2
  )
  AND deleted IS FALSE;

UPDATE risk_policies
SET deleted_at = clock_timestamp(), updated_at = clock_timestamp()
WHERE project_id IN (SELECT id FROM reset_project)
  AND deleted IS FALSE
  AND policy_type = 'standard'
  AND action = 'block'
  AND audience_type = 'everyone'
  AND auto_name IS TRUE
  AND sources = ARRAY['gitleaks']::text[]
  AND message_types @> ARRAY['tool_request', 'tool_response']::text[]
  AND cardinality(message_types) = 2;

COMMIT;
SQL

echo "Project-home journey reset complete for project: $project_slug"
