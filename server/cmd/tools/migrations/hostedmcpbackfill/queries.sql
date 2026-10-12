-- name: ListCandidateToolsets :many
SELECT t.id, t.project_id
FROM toolsets AS t
JOIN projects AS p ON p.id = t.project_id AND p.organization_id = t.organization_id AND p.deleted IS FALSE
WHERE t.deleted IS FALSE
  AND t.mcp_slug IS NOT NULL
  AND t.mcp_slug <> ''
  AND (sqlc.narg('project_id')::uuid IS NULL OR t.project_id = sqlc.narg('project_id')::uuid)
  AND t.id > @after_id::uuid
ORDER BY t.id
LIMIT @page_size::int;

-- name: LockToolsetBackfill :exec
SELECT pg_advisory_xact_lock(hashtextextended('hosted-mcp-backfill:' || @toolset_id::text, 0));

-- name: LiveProjectExists :one
SELECT EXISTS (
  SELECT 1
  FROM projects
  WHERE id = @id AND organization_id = @organization_id AND deleted IS FALSE
);

-- name: GetServerIdentity :one
-- Any state: a tombstoned row still owns its id.
SELECT id, toolset_id, deleted
FROM mcp_servers
WHERE id = @id AND project_id = @project_id;

-- name: CountFreshIDServers :one
SELECT count(*)
FROM mcp_servers
WHERE project_id = @project_id
  AND toolset_id = @toolset_id::uuid
  AND id <> @toolset_id::uuid
  AND deleted IS FALSE;

-- name: LiveCustomDomainExists :one
SELECT EXISTS (
  SELECT 1
  FROM custom_domains
  WHERE id = @id AND organization_id = @organization_id AND deleted IS FALSE
);

-- name: EndpointAddressHeldElsewhere :one
-- Addresses are a global namespace, mirroring the partial unique indexes, so
-- this probe is deliberately not project-scoped.
SELECT EXISTS (
  SELECT 1
  FROM mcp_endpoints
  WHERE slug = @slug
    AND custom_domain_id IS NOT DISTINCT FROM sqlc.narg('custom_domain_id')::uuid
    AND deleted IS FALSE
    AND mcp_server_id IS DISTINCT FROM @mcp_server_id::uuid
);

-- name: ServerSlugHeldElsewhere :one
SELECT EXISTS (
  SELECT 1
  FROM mcp_servers
  WHERE project_id = @project_id
    AND slug = @slug
    AND deleted IS FALSE
    AND id <> @id
);

-- name: ListDependentToolsets :many
-- Live toolset-keyed dependents, grouped by the toolset they point at.
SELECT DISTINCT d.toolset_id::uuid AS toolset_id
FROM (
  SELECT m.toolset_id, m.project_id
  FROM mcp_metadata AS m
  WHERE m.toolset_id IS NOT NULL
  UNION ALL
  SELECT ps.toolset_id, pl.project_id
  FROM plugin_servers AS ps
  JOIN plugins AS pl ON pl.id = ps.plugin_id
  WHERE ps.toolset_id IS NOT NULL AND ps.deleted IS FALSE
  UNION ALL
  SELECT a.toolset_id, a.project_id
  FROM assistant_toolsets AS a
) AS d
WHERE (sqlc.narg('project_id')::uuid IS NULL OR d.project_id = sqlc.narg('project_id')::uuid)
  AND d.toolset_id > @after_id::uuid
ORDER BY 1
LIMIT @page_size::int;

-- name: ListToolsetDependents :many
SELECT d.table_name::text AS table_name, d.id::uuid AS id, d.project_id::uuid AS project_id
FROM (
  SELECT 'mcp_metadata' AS table_name, m.id, m.project_id
  FROM mcp_metadata AS m
  WHERE m.toolset_id = @toolset_id::uuid
  UNION ALL
  SELECT 'plugin_servers', ps.id, pl.project_id
  FROM plugin_servers AS ps
  JOIN plugins AS pl ON pl.id = ps.plugin_id
  WHERE ps.toolset_id = @toolset_id::uuid AND ps.deleted IS FALSE
  UNION ALL
  SELECT 'assistant_toolsets', a.id, a.project_id
  FROM assistant_toolsets AS a
  WHERE a.toolset_id = @toolset_id::uuid
) AS d
WHERE sqlc.narg('project_id')::uuid IS NULL OR d.project_id = sqlc.narg('project_id')::uuid
ORDER BY 1, 2;

-- name: LockDependentToolset :one
SELECT t.id, t.project_id, t.organization_id, t.mcp_slug, t.deleted, p.deleted AS project_deleted
FROM toolsets AS t
JOIN projects AS p ON p.id = t.project_id AND p.organization_id = t.organization_id
WHERE t.id = @id AND t.project_id = @project_id
FOR NO KEY UPDATE OF t;

-- name: GetLiveCanonicalWrapper :one
SELECT
  ms.id,
  ms.name,
  ms.slug,
  ms.visibility,
  EXISTS (
    SELECT 1
    FROM mcp_endpoints AS me
    WHERE me.mcp_server_id = ms.id
      AND me.project_id = ms.project_id
      AND me.custom_domain_id IS NULL
      AND me.deleted IS FALSE
  ) AS has_platform_endpoint
FROM mcp_servers AS ms
WHERE ms.id = @toolset_id::uuid
  AND ms.project_id = @project_id
  AND ms.toolset_id = @toolset_id::uuid
  AND ms.deleted IS FALSE
FOR SHARE OF ms;

-- name: LockToolsetKeyedMetadata :one
SELECT *
FROM mcp_metadata
WHERE id = @id AND project_id = @project_id AND toolset_id = @toolset_id
FOR UPDATE;

-- name: ServerKeyedMetadataExists :one
SELECT EXISTS (
  SELECT 1
  FROM mcp_metadata
  WHERE mcp_server_id = @mcp_server_id AND project_id = @project_id
);

-- name: RekeyMetadata :one
UPDATE mcp_metadata
SET toolset_id = NULL, mcp_server_id = @mcp_server_id, updated_at = clock_timestamp()
WHERE id = @id AND project_id = @project_id AND toolset_id = @mcp_server_id
RETURNING *;

-- name: ListMetadataEnvironmentConfigs :many
SELECT *
FROM mcp_environment_configs
WHERE mcp_metadata_id = @mcp_metadata_id AND project_id = @project_id
ORDER BY variable_name;

-- name: LockToolsetKeyedPluginServer :one
SELECT ps.id, ps.plugin_id, ps.display_name, ps.policy, ps.sort_order, pl.name AS plugin_name, pl.slug AS plugin_slug
FROM plugin_servers AS ps
JOIN plugins AS pl ON pl.id = ps.plugin_id
WHERE ps.id = @id
  AND pl.project_id = @project_id
  AND ps.toolset_id = @toolset_id
  AND ps.deleted IS FALSE
FOR UPDATE OF ps;

-- name: ServerKeyedPluginServerExists :one
SELECT EXISTS (
  SELECT 1
  FROM plugin_servers AS ps
  JOIN plugins AS pl ON pl.id = ps.plugin_id
  WHERE ps.plugin_id = @plugin_id
    AND pl.project_id = @project_id
    AND ps.mcp_server_id = @mcp_server_id
    AND ps.deleted IS FALSE
);

-- name: RekeyPluginServer :execrows
UPDATE plugin_servers AS ps
SET toolset_id = NULL, mcp_server_id = @mcp_server_id, updated_at = clock_timestamp()
FROM plugins AS pl
WHERE ps.id = @id
  AND pl.id = ps.plugin_id
  AND pl.project_id = @project_id
  AND ps.toolset_id = @mcp_server_id
  AND ps.deleted IS FALSE;

-- name: GetAssistantToolsetOwner :one
SELECT assistant_id
FROM assistant_toolsets
WHERE id = @id AND project_id = @project_id AND toolset_id = @toolset_id;

-- name: LockAssistantForMove :exec
-- Same row lock as UpdateAssistant, so an in-flight edit of the attachments finishes first.
SELECT id
FROM assistants
WHERE id = @id AND project_id = @project_id
FOR NO KEY UPDATE;

-- name: LockAssistantToolset :one
SELECT id, assistant_id
FROM assistant_toolsets
WHERE id = @id AND project_id = @project_id AND toolset_id = @toolset_id
FOR UPDATE;

-- name: AssistantMcpServerExists :one
SELECT EXISTS (
  SELECT 1
  FROM assistant_mcp_servers
  WHERE assistant_id = @assistant_id AND mcp_server_id = @mcp_server_id AND project_id = @project_id
);

-- name: MoveAssistantToolset :execrows
-- Same id, environment and created_at, so the attachment is the same row in its new table.
WITH moved AS (
  DELETE FROM assistant_toolsets AS at
  WHERE at.id = @id AND at.project_id = @project_id AND at.toolset_id = @mcp_server_id::uuid
  RETURNING at.id, at.assistant_id, at.environment_id, at.project_id, at.created_at
)
INSERT INTO assistant_mcp_servers (id, assistant_id, mcp_server_id, environment_id, project_id, created_at)
SELECT moved.id, moved.assistant_id, @mcp_server_id::uuid, moved.environment_id, moved.project_id, moved.created_at
FROM moved;

-- TEST FIXTURE ONLY: every query below is for package tests and may create impossible states.

-- name: SeedOrganizationFixture :exec
INSERT INTO organization_metadata (id, name, slug)
VALUES (@id, @name, @slug);

-- name: SeedProjectFixture :exec
INSERT INTO projects (id, name, slug, organization_id)
VALUES (@id, @name, @slug, @organization_id);

-- name: SeedCustomDomainFixture :exec
INSERT INTO custom_domains (id, organization_id, domain, verified, activated, deleted_at)
VALUES (@id, @organization_id, @domain, TRUE, TRUE, @deleted_at);

-- name: SeedToolsetFixture :exec
INSERT INTO toolsets (
  id, organization_id, project_id, name, slug, mcp_slug, mcp_is_public, mcp_enabled, custom_domain_id
) VALUES (
  @id, @organization_id, @project_id, @name, @slug, @mcp_slug, @mcp_is_public, @mcp_enabled, @custom_domain_id
);

-- name: SeedServerFixture :exec
INSERT INTO mcp_servers (id, project_id, name, slug, toolset_id, visibility, network_access_mode, deleted_at)
VALUES (@id, @project_id, @name, @slug, @toolset_id, @visibility, @network_access_mode, @deleted_at);

-- name: SoftDeleteProjectFixture :exec
UPDATE projects SET deleted_at = clock_timestamp()
WHERE id = @id AND organization_id = @organization_id;

-- name: SeedEndpointFixture :exec
INSERT INTO mcp_endpoints (id, project_id, custom_domain_id, mcp_server_id, slug)
VALUES (@id, @project_id, @custom_domain_id, @mcp_server_id, @slug);

-- name: ListServersFixture :many
SELECT *
FROM mcp_servers
WHERE project_id = @project_id
ORDER BY id;

-- name: ListEndpointsFixture :many
SELECT *
FROM mcp_endpoints
WHERE project_id = @project_id
ORDER BY id;

-- name: ListAuditActorsFixture :many
SELECT actor_id, actor_type, action
FROM audit_logs
WHERE organization_id = @organization_id
ORDER BY seq;

-- name: SeedMetadataFixture :exec
INSERT INTO mcp_metadata (id, toolset_id, mcp_server_id, project_id)
VALUES (@id, @toolset_id, @mcp_server_id, @project_id);

-- name: SeedEnvironmentConfigFixture :exec
INSERT INTO mcp_environment_configs (id, project_id, mcp_metadata_id, variable_name)
VALUES (@id, @project_id, @mcp_metadata_id, @variable_name);

-- name: SeedPluginFixture :exec
INSERT INTO plugins (id, organization_id, project_id, name, slug)
VALUES (@id, @organization_id, @project_id, @name, @slug);

-- name: SeedPluginServerFixture :exec
INSERT INTO plugin_servers (id, plugin_id, toolset_id, mcp_server_id, display_name)
VALUES (@id, @plugin_id, @toolset_id, @mcp_server_id, @display_name);

-- name: SeedAssistantFixture :exec
INSERT INTO assistants (id, project_id, organization_id, name, model, instructions)
VALUES (@id, @project_id, @organization_id, @name, 'model', '');

-- name: SeedAssistantToolsetFixture :exec
INSERT INTO assistant_toolsets (id, assistant_id, toolset_id, project_id)
VALUES (@id, @assistant_id, @toolset_id, @project_id);

-- name: SeedAssistantMcpServerFixture :exec
INSERT INTO assistant_mcp_servers (id, assistant_id, mcp_server_id, project_id)
VALUES (@id, @assistant_id, @mcp_server_id, @project_id);

-- name: SoftDeleteToolsetFixture :exec
UPDATE toolsets SET deleted_at = clock_timestamp()
WHERE id = @id AND project_id = @project_id;

-- name: ListMetadataFixture :many
SELECT id, toolset_id, mcp_server_id
FROM mcp_metadata
WHERE project_id = @project_id
ORDER BY id;

-- name: ListEnvironmentConfigsFixture :many
SELECT id, mcp_metadata_id, variable_name
FROM mcp_environment_configs
WHERE project_id = @project_id
ORDER BY id;

-- name: ListPluginServersFixture :many
SELECT ps.id, ps.toolset_id, ps.mcp_server_id
FROM plugin_servers AS ps
JOIN plugins AS pl ON pl.id = ps.plugin_id
WHERE pl.project_id = @project_id
ORDER BY ps.id;

-- name: ListAssistantToolsetsFixture :many
SELECT id, assistant_id, toolset_id
FROM assistant_toolsets
WHERE project_id = @project_id
ORDER BY id;

-- name: ListAssistantMcpServersFixture :many
SELECT id, assistant_id, mcp_server_id
FROM assistant_mcp_servers
WHERE project_id = @project_id
ORDER BY id;

-- name: CountOutboxFixture :one
SELECT count(*)
FROM publish_outbox
WHERE organization_id = @organization_id;
