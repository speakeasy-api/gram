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

-- name: SetMoveLockTimeout :exec
SELECT set_config('lock_timeout', @lock_timeout::text, true);

-- name: LockDependentToolset :one
SELECT t.id, t.project_id, t.organization_id, t.mcp_slug, t.mcp_enabled, t.deleted, p.deleted AS project_deleted
FROM toolsets AS t
JOIN projects AS p ON p.id = t.project_id AND p.organization_id = t.organization_id
WHERE t.id = @id AND t.project_id = @project_id
FOR NO KEY UPDATE OF t;

-- name: GetLiveCanonicalWrapper :one
SELECT ms.id, ms.name, ms.slug
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

-- name: LockServerKeyedMetadataTwin :one
SELECT m.id, to_jsonb(m)::jsonb AS row_json
FROM mcp_metadata AS m
WHERE m.mcp_server_id = @mcp_server_id AND m.project_id = @project_id
FOR UPDATE;

-- name: MetadataEnvironmentConfigsJSON :one
SELECT COALESCE(jsonb_agg(to_jsonb(c) ORDER BY c.variable_name), '[]'::jsonb)::jsonb AS configs
FROM mcp_environment_configs AS c
WHERE c.mcp_metadata_id = @mcp_metadata_id AND c.project_id = @project_id;

-- name: DeleteMetadataTwin :execrows
-- Cascades to the twin's mcp_environment_configs, which the journal records.
DELETE FROM mcp_metadata
WHERE id = @id AND project_id = @project_id AND mcp_server_id = @mcp_server_id;

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

-- name: LockServerKeyedPluginServerTwin :one
SELECT
  ps.id,
  to_jsonb(ps)::jsonb AS row_json,
  EXISTS (
    SELECT 1
    FROM platform_mcp_distributions AS d
    WHERE d.project_id = pl.project_id AND d.plugin_server_id = ps.id
  ) AS distributed
FROM plugin_servers AS ps
JOIN plugins AS pl ON pl.id = ps.plugin_id
WHERE ps.plugin_id = @plugin_id
  AND pl.project_id = @project_id
  AND ps.mcp_server_id = @mcp_server_id
  AND ps.deleted IS FALSE
FOR UPDATE OF ps;

-- name: SoftDeletePluginServerTwin :execrows
UPDATE plugin_servers AS ps
SET deleted_at = clock_timestamp(), updated_at = clock_timestamp()
FROM plugins AS pl
WHERE ps.id = @id
  AND pl.id = ps.plugin_id
  AND pl.project_id = @project_id
  AND ps.mcp_server_id = @mcp_server_id
  AND ps.deleted IS FALSE;

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

-- name: LockAssistantForMove :one
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

-- name: LockAssistantMcpServerTwin :one
SELECT ams.id, to_jsonb(ams)::jsonb AS row_json
FROM assistant_mcp_servers AS ams
WHERE ams.assistant_id = @assistant_id AND ams.mcp_server_id = @mcp_server_id AND ams.project_id = @project_id
FOR UPDATE;

-- name: DeleteAssistantMcpServerTwin :execrows
DELETE FROM assistant_mcp_servers
WHERE id = @id AND project_id = @project_id AND mcp_server_id = @mcp_server_id;

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

-- name: LockOrphanToolset :one
-- FOR SHARE OF p holds off a concurrent project delete or restore until commit.
SELECT t.id, t.project_id, t.organization_id, t.name, t.slug, t.mcp_slug, t.deleted, p.deleted AS project_deleted
FROM toolsets AS t
JOIN projects AS p ON p.id = t.project_id AND p.organization_id = t.organization_id
WHERE t.id = @id AND t.project_id = @project_id
FOR NO KEY UPDATE OF t FOR SHARE OF p;

-- name: CanonicalWrapperLive :one
SELECT EXISTS (
  SELECT 1
  FROM mcp_servers AS ms
  WHERE ms.id = @toolset_id::uuid
    AND ms.project_id = @project_id
    AND ms.toolset_id = @toolset_id::uuid
    AND ms.deleted IS FALSE
);

-- name: LockOrphanMetadata :one
SELECT sqlc.embed(m), to_jsonb(m)::jsonb AS row_json
FROM mcp_metadata AS m
WHERE m.id = @id AND m.project_id = @project_id AND m.toolset_id = @toolset_id::uuid
FOR UPDATE OF m;

-- name: LockMetadataEnvironmentConfigs :many
SELECT *
FROM mcp_environment_configs
WHERE mcp_metadata_id = @mcp_metadata_id AND project_id = @project_id
ORDER BY variable_name
FOR UPDATE;

-- name: MetadataHasForeignEnvironmentConfig :one
-- The cascade would remove these too, so a config outside the project blocks the delete.
SELECT EXISTS (
  SELECT 1
  FROM mcp_environment_configs
  WHERE mcp_metadata_id = @mcp_metadata_id AND project_id <> @project_id
);

-- name: DeleteOrphanMetadata :execrows
-- Cascades to the row's mcp_environment_configs, which the journal records.
DELETE FROM mcp_metadata
WHERE id = @id AND project_id = @project_id AND toolset_id = @toolset_id::uuid;

-- name: LockOrphanPluginServer :one
SELECT
  ps.id,
  ps.plugin_id,
  pl.name AS plugin_name,
  pl.slug AS plugin_slug,
  to_jsonb(ps)::jsonb AS row_json,
  EXISTS (
    SELECT 1
    FROM platform_mcp_distributions AS d
    WHERE d.project_id = pl.project_id AND d.plugin_server_id = ps.id
  ) AS distributed
FROM plugin_servers AS ps
JOIN plugins AS pl ON pl.id = ps.plugin_id
WHERE ps.id = @id
  AND pl.project_id = @project_id
  AND ps.toolset_id = @toolset_id::uuid
  AND ps.deleted IS FALSE
FOR UPDATE OF ps;

-- name: SoftDeleteOrphanPluginServer :one
-- deleted_at as text round-trips exactly, so a restore can match only this tool's delete.
UPDATE plugin_servers AS ps
SET deleted_at = clock_timestamp(), updated_at = clock_timestamp()
FROM plugins AS pl
WHERE ps.id = @id
  AND pl.id = ps.plugin_id
  AND pl.project_id = @project_id
  AND ps.toolset_id = @toolset_id::uuid
  AND ps.deleted IS FALSE
RETURNING ps.deleted_at::text AS deleted_at;

-- name: LockOrphanAssistantToolset :one
SELECT a.id, a.assistant_id, to_jsonb(a)::jsonb AS row_json
FROM assistant_toolsets AS a
WHERE a.id = @id AND a.project_id = @project_id AND a.toolset_id = @toolset_id::uuid
FOR UPDATE OF a;

-- name: DeleteOrphanAssistantToolset :execrows
DELETE FROM assistant_toolsets
WHERE id = @id AND project_id = @project_id AND toolset_id = @toolset_id::uuid;

-- name: ListRemovalRecordToolsets :many
-- Live rows are listed too, so the run reports them as blocked_live.
SELECT DISTINCT ps.toolset_id::uuid AS toolset_id
FROM plugin_servers AS ps
JOIN plugins AS pl ON pl.id = ps.plugin_id
WHERE ps.toolset_id IS NOT NULL
  AND ps.mcp_server_id IS NULL
  AND ps.meta_mcp_server_id IS NULL
  AND (sqlc.narg('project_id')::uuid IS NULL OR pl.project_id = sqlc.narg('project_id')::uuid)
  AND ps.toolset_id > @after_id::uuid
ORDER BY 1
LIMIT @page_size::int;

-- name: ListToolsetRemovalRecords :many
SELECT ps.id, pl.project_id
FROM plugin_servers AS ps
JOIN plugins AS pl ON pl.id = ps.plugin_id
WHERE ps.toolset_id = @toolset_id::uuid
  AND ps.mcp_server_id IS NULL
  AND ps.meta_mcp_server_id IS NULL
  AND (sqlc.narg('project_id')::uuid IS NULL OR pl.project_id = sqlc.narg('project_id')::uuid)
ORDER BY ps.id;

-- name: LockToolsetKeyedRemovalRecord :one
SELECT ps.id, ps.plugin_id, ps.deleted, COALESCE(ps.deleted_at::text, '')::text AS deleted_at, to_jsonb(ps)::jsonb AS row_json
FROM plugin_servers AS ps
JOIN plugins AS pl ON pl.id = ps.plugin_id
WHERE ps.id = @id
  AND pl.project_id = @project_id
  AND ps.toolset_id = @toolset_id::uuid
  AND ps.mcp_server_id IS NULL
  AND ps.meta_mcp_server_id IS NULL
FOR UPDATE OF ps;

-- name: LockServerKeyedRemovalRecord :one
SELECT ps.id
FROM plugin_servers AS ps
JOIN plugins AS pl ON pl.id = ps.plugin_id
WHERE ps.plugin_id = @plugin_id
  AND pl.project_id = @project_id
  AND ps.mcp_server_id = @mcp_server_id::uuid
  AND ps.deleted IS TRUE
ORDER BY ps.id
LIMIT 1
FOR UPDATE OF ps;

-- name: RekeyRemovalRecord :one
-- Soft-deleted rows only: every partial unique index on plugin_servers covers live rows, so none applies.
UPDATE plugin_servers AS ps
SET toolset_id = NULL, mcp_server_id = @mcp_server_id::uuid, updated_at = clock_timestamp()
FROM plugins AS pl
WHERE ps.id = @id
  AND pl.id = ps.plugin_id
  AND pl.project_id = @project_id
  AND ps.toolset_id = @mcp_server_id::uuid
  AND ps.mcp_server_id IS NULL
  AND ps.meta_mcp_server_id IS NULL
  AND ps.deleted IS TRUE
RETURNING ps.deleted_at::text AS deleted_at;

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

-- name: SoftDeleteEndpointFixture :exec
UPDATE mcp_endpoints SET deleted_at = clock_timestamp()
WHERE id = @id AND project_id = @project_id;

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
INSERT INTO mcp_metadata (id, toolset_id, mcp_server_id, project_id, default_environment_id, instructions)
VALUES (@id, @toolset_id, @mcp_server_id, @project_id, @default_environment_id, @instructions);

-- name: SeedEnvironmentFixture :exec
INSERT INTO environments (id, organization_id, project_id, name, slug)
VALUES (@id, @organization_id, @project_id, @name, @slug);

-- name: SeedDistributionReferenceFixture :exec
WITH registration AS (
  INSERT INTO platform_mcp_catalog_registrations (organization_id, project_id, source_kind, catalog_provider, catalog_reference)
  VALUES (@organization_id, @project_id, 'fixture', 'fixture', 'fixture')
  RETURNING id
)
INSERT INTO platform_mcp_distributions (
  organization_id, project_id, registration_id, default_plugin_id, plugin_server_id, state, version,
  attachment_was_created, user_id, acting_surface
)
SELECT @organization_id, @project_id, registration.id, @plugin_id, @plugin_server_id, 'active', 1, FALSE, 'user_fixture', 'dashboard'
FROM registration;

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
INSERT INTO assistant_toolsets (id, assistant_id, toolset_id, project_id, environment_id)
VALUES (@id, @assistant_id, @toolset_id, @project_id, @environment_id);

-- name: SeedAssistantMcpServerFixture :exec
INSERT INTO assistant_mcp_servers (id, assistant_id, mcp_server_id, project_id)
VALUES (@id, @assistant_id, @mcp_server_id, @project_id);

-- name: SoftDeleteToolsetFixture :exec
UPDATE toolsets SET deleted_at = clock_timestamp()
WHERE id = @id AND project_id = @project_id;

-- name: ListMetadataFixture :many
SELECT id, toolset_id, mcp_server_id, default_environment_id, instructions
FROM mcp_metadata
WHERE project_id = @project_id
ORDER BY id;

-- name: ListEnvironmentConfigsFixture :many
SELECT id, mcp_metadata_id, variable_name
FROM mcp_environment_configs
WHERE project_id = @project_id
ORDER BY id;

-- name: ListPluginServersFixture :many
SELECT ps.id, ps.toolset_id, ps.mcp_server_id, ps.deleted
FROM plugin_servers AS ps
JOIN plugins AS pl ON pl.id = ps.plugin_id
WHERE pl.project_id = @project_id
ORDER BY ps.id;

-- name: ListAssistantToolsetsFixture :many
SELECT id, assistant_id, toolset_id, environment_id
FROM assistant_toolsets
WHERE project_id = @project_id
ORDER BY id;

-- name: ListAssistantMcpServersFixture :many
SELECT id, assistant_id, mcp_server_id, environment_id
FROM assistant_mcp_servers
WHERE project_id = @project_id
ORDER BY id;

-- name: ListAuditSnapshotsFixture :many
SELECT action, before_snapshot, after_snapshot, metadata
FROM audit_logs
WHERE organization_id = @organization_id
ORDER BY seq;

-- name: RollbackMetadataMoveFixture :execrows
-- The runbook's rollback statements, run from journal values.
UPDATE mcp_metadata SET mcp_server_id = NULL, toolset_id = @toolset_id::uuid
WHERE id = @id AND project_id = @project_id AND mcp_server_id = @toolset_id::uuid;

-- name: RestoreMetadataTwinFixture :exec
INSERT INTO mcp_metadata
SELECT (r).* FROM jsonb_populate_record(NULL::mcp_metadata, @row_json::jsonb) AS r
WHERE r.project_id = @project_id::uuid
ON CONFLICT (id) DO NOTHING;

-- name: RestoreEnvironmentConfigsFixture :exec
INSERT INTO mcp_environment_configs
SELECT (c).* FROM jsonb_populate_recordset(NULL::mcp_environment_configs, @rows_json::jsonb) AS c
WHERE c.project_id = @project_id::uuid
ON CONFLICT (id) DO NOTHING;

-- name: RestoreProjectFixture :exec
UPDATE projects SET deleted_at = NULL
WHERE id = @id AND organization_id = @organization_id;

-- name: SeedPluginGithubConnectionFixture :exec
INSERT INTO plugin_github_connections (project_id, installation_id, repo_owner, repo_name)
VALUES (@project_id, 1, 'owner', 'repo');

-- name: BumpPluginServerDeletedAtFixture :exec
UPDATE plugin_servers AS ps SET deleted_at = ps.deleted_at + interval '1 second'
FROM plugins AS pl
WHERE ps.id = @id AND pl.id = ps.plugin_id AND pl.project_id = @project_id;

-- name: SeedRemovedPluginServerFixture :exec
INSERT INTO plugin_servers (id, plugin_id, toolset_id, mcp_server_id, display_name, deleted_at)
VALUES (@id, @plugin_id, @toolset_id, @mcp_server_id, @display_name, @deleted_at);

-- name: ListPluginServerRemovalsFixture :many
SELECT ps.id, ps.plugin_id, ps.toolset_id, ps.mcp_server_id, ps.deleted, COALESCE(ps.deleted_at::text, '')::text AS deleted_at
FROM plugin_servers AS ps
JOIN plugins AS pl ON pl.id = ps.plugin_id
WHERE pl.project_id = @project_id
ORDER BY ps.id;

-- name: CountOutboxFixture :one
SELECT count(*)
FROM publish_outbox
WHERE organization_id = @organization_id;
