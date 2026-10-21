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
-- distributed lets a caller report blocked_distributed rather than let the NO ACTION FK fail its delete and stop the run.
SELECT
  ps.id,
  ps.plugin_id,
  ps.deleted,
  COALESCE(ps.deleted_at::text, '')::text AS deleted_at,
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
RETURNING ps.deleted_at::text AS deleted_at, ps.updated_at::text AS updated_at;

-- name: LockLiveToolsetServers :many
SELECT ms.id
FROM mcp_servers AS ms
WHERE ms.project_id = @project_id
  AND ms.toolset_id = @toolset_id::uuid
  AND ms.deleted IS FALSE
ORDER BY ms.id
FOR SHARE OF ms;

-- name: DeleteLeftoverRemovalRecord :execrows
-- Repeats the caller's eligibility checks so a concurrent change deletes nothing.
DELETE FROM plugin_servers AS ps
USING plugins AS pl
WHERE ps.id = @id
  AND pl.id = ps.plugin_id
  AND pl.project_id = @project_id
  AND ps.toolset_id = @toolset_id::uuid
  AND ps.mcp_server_id IS NULL
  AND ps.meta_mcp_server_id IS NULL
  AND ps.deleted IS TRUE
  AND NOT EXISTS (
    SELECT 1
    FROM platform_mcp_distributions AS d
    WHERE d.project_id = pl.project_id AND d.plugin_server_id = ps.id
  )
  AND CASE @eligibility::text
    WHEN 'project_deleted' THEN EXISTS (
      SELECT 1 FROM projects AS p WHERE p.id = pl.project_id AND p.deleted IS TRUE
    )
    WHEN 'toolset_deleted' THEN NOT EXISTS (
      SELECT 1
      FROM mcp_servers AS ms
      WHERE ms.project_id = pl.project_id AND ms.toolset_id = ps.toolset_id AND ms.deleted IS FALSE
    )
    WHEN 'duplicate' THEN EXISTS (
      SELECT 1
      FROM mcp_servers AS ms
      WHERE ms.id = ps.toolset_id AND ms.project_id = pl.project_id AND ms.toolset_id = ps.toolset_id AND ms.deleted IS FALSE
    ) AND NOT EXISTS (
      SELECT 1
      FROM mcp_servers AS ms
      WHERE ms.project_id = pl.project_id
        AND ms.toolset_id = ps.toolset_id
        AND ms.deleted IS FALSE
        AND NOT EXISTS (
          SELECT 1
          FROM plugin_servers AS r
          WHERE r.plugin_id = ps.plugin_id AND r.mcp_server_id = ms.id AND r.deleted IS TRUE
        )
    )
    ELSE FALSE
  END;

-- name: ListOrphanWrapperToolsets :many
-- Deleted toolsets of live projects that still back a live server; canonical rows are listed so the run reports them.
SELECT DISTINCT ms.toolset_id::uuid AS toolset_id
FROM mcp_servers AS ms
JOIN toolsets AS t ON t.id = ms.toolset_id AND t.project_id = ms.project_id
JOIN projects AS p ON p.id = ms.project_id AND p.organization_id = t.organization_id
WHERE ms.deleted IS FALSE
  AND t.deleted IS TRUE
  AND p.deleted IS FALSE
  AND (sqlc.narg('project_id')::uuid IS NULL OR ms.project_id = sqlc.narg('project_id')::uuid)
  AND ms.toolset_id > @after_id::uuid
ORDER BY 1
LIMIT @page_size::int;

-- name: ListToolsetOrphanWrappers :many
SELECT ms.id, ms.project_id
FROM mcp_servers AS ms
WHERE ms.toolset_id = @toolset_id::uuid
  AND ms.deleted IS FALSE
  AND (sqlc.narg('project_id')::uuid IS NULL OR ms.project_id = sqlc.narg('project_id')::uuid)
ORDER BY ms.id;

-- name: CountLiveServerEndpoints :one
SELECT count(*)
FROM mcp_endpoints AS e
WHERE e.mcp_server_id = @mcp_server_id::uuid AND e.project_id = @project_id AND e.deleted IS FALSE;

-- name: WrapperServerImage :one
SELECT to_jsonb(ms)::jsonb AS row_json
FROM mcp_servers AS ms
WHERE ms.id = @id AND ms.project_id = @project_id;

-- name: WrapperServerDeletedAt :one
SELECT COALESCE(ms.deleted_at::text, '')::text AS deleted_at
FROM mcp_servers AS ms
WHERE ms.id = @id AND ms.project_id = @project_id;

-- name: LockWrapperMemberships :many
SELECT m.id, to_jsonb(m)::jsonb AS row_json
FROM meta_mcp_server_members AS m
WHERE m.mcp_server_id = @mcp_server_id AND m.project_id = @project_id AND m.deleted IS FALSE
ORDER BY m.id
FOR UPDATE OF m;

-- name: WrapperMembershipsDeletedAt :many
SELECT m.id, COALESCE(m.deleted_at::text, '')::text AS deleted_at
FROM meta_mcp_server_members AS m
WHERE m.id = ANY(@ids::uuid[]) AND m.project_id = @project_id
ORDER BY m.id;

-- name: LockWrapperPluginMembers :many
SELECT ps.id, to_jsonb(ps)::jsonb AS row_json
FROM plugin_servers AS ps
JOIN plugins AS pl ON pl.id = ps.plugin_id
WHERE ps.mcp_server_id = @mcp_server_id::uuid AND pl.project_id = @project_id AND ps.deleted IS FALSE
ORDER BY ps.id
FOR UPDATE OF ps;

-- name: WrapperPluginMembersDeletedAt :many
SELECT ps.id, COALESCE(ps.deleted_at::text, '')::text AS deleted_at
FROM plugin_servers AS ps
JOIN plugins AS pl ON pl.id = ps.plugin_id
WHERE ps.id = ANY(@ids::uuid[]) AND pl.project_id = @project_id
ORDER BY ps.id;

-- name: LockWrapperAssistantAttachments :many
SELECT a.id, to_jsonb(a)::jsonb AS row_json
FROM assistant_mcp_servers AS a
WHERE a.mcp_server_id = @mcp_server_id AND a.project_id = @project_id
ORDER BY a.id
FOR UPDATE OF a;

-- name: CountWrapperAssistantAttachments :one
SELECT count(*)
FROM assistant_mcp_servers AS a
WHERE a.id = ANY(@ids::uuid[]) AND a.project_id = @project_id;

-- name: PruneUnservableSessionClientLinks :many
-- PruneUnservableAssociations verbatim, as the app's post-delete issuer resync runs it, returning what it deleted.
DELETE FROM mcp_server_remote_session_clients AS a
USING mcp_servers AS s
WHERE a.mcp_server_id = s.id
  AND a.project_id = @project_id::uuid
  AND a.organization_id = @organization_id::text
  AND a.deleted IS FALSE
  AND (
    s.deleted IS TRUE
    OR NOT EXISTS (
      SELECT 1
      FROM remote_session_client_user_session_issuers AS link
      JOIN remote_session_clients AS c
        ON c.id = link.remote_session_client_id
       AND c.deleted IS FALSE
      WHERE link.remote_session_client_id = a.remote_session_client_id
        AND (
          link.user_session_issuer_id = s.user_session_issuer_id
          OR EXISTS (
            SELECT 1
            FROM meta_mcp_servers AS gateway
            JOIN projects AS gateway_project
              ON gateway_project.id = gateway.project_id
             AND gateway_project.deleted IS FALSE
            JOIN user_session_issuers AS gateway_issuer
              ON gateway_issuer.id = gateway.user_session_issuer_id
             AND gateway_issuer.deleted IS FALSE
             AND (gateway_issuer.project_id = s.project_id
                  OR (gateway_issuer.project_id IS NULL AND gateway_issuer.organization_id = a.organization_id))
            JOIN meta_mcp_server_members AS member
              ON member.meta_mcp_server_id = gateway.id
             AND member.project_id = gateway.project_id
             AND member.deleted IS FALSE
            WHERE gateway.user_session_issuer_id = link.user_session_issuer_id
              AND gateway.deleted IS FALSE
              AND member.mcp_server_id = s.id
          )
          OR EXISTS (
            SELECT 1
            FROM user_session_issuers AS organization_issuer
            WHERE organization_issuer.id = link.user_session_issuer_id
              AND organization_issuer.project_id IS NULL
              AND organization_issuer.organization_id = a.organization_id
              AND organization_issuer.deleted IS FALSE
              AND (
              SELECT count(*)
              FROM user_session_issuers AS sibling
              WHERE sibling.organization_id = organization_issuer.organization_id
                AND sibling.project_id IS NULL
                AND sibling.deleted IS FALSE
            ) = 1
          )
        )
    )
  )
RETURNING a.id, a.mcp_server_id, to_jsonb(a)::jsonb AS row_json;

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

-- name: SoftDeleteServerFixture :exec
UPDATE mcp_servers SET deleted_at = clock_timestamp()
WHERE id = @id AND project_id = @project_id;

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
VALUES (@project_id::uuid, 1, 'owner', 'repo-' || (@project_id::uuid)::text);

-- name: BumpPluginServerDeletedAtFixture :exec
UPDATE plugin_servers AS ps SET deleted_at = ps.deleted_at + interval '1 second'
FROM plugins AS pl
WHERE ps.id = @id AND pl.id = ps.plugin_id AND pl.project_id = @project_id;

-- name: BumpPluginServerUpdatedAtFixture :exec
UPDATE plugin_servers AS ps SET updated_at = ps.updated_at + interval '1 second'
FROM plugins AS pl
WHERE ps.id = @id AND pl.id = ps.plugin_id AND pl.project_id = @project_id;

-- name: SeedRemovedPluginServerFixture :exec
INSERT INTO plugin_servers (id, plugin_id, toolset_id, mcp_server_id, display_name, deleted_at)
VALUES (@id, @plugin_id, @toolset_id, @mcp_server_id, @display_name, @deleted_at);

-- name: ListPluginServerRemovalsFixture :many
SELECT ps.id, ps.plugin_id, ps.toolset_id, ps.mcp_server_id, ps.deleted, COALESCE(ps.deleted_at::text, '')::text AS deleted_at, ps.updated_at::text AS updated_at
FROM plugin_servers AS ps
JOIN plugins AS pl ON pl.id = ps.plugin_id
WHERE pl.project_id = @project_id
ORDER BY ps.id;

-- name: CountOutboxFixture :one
SELECT count(*)
FROM publish_outbox
WHERE organization_id = @organization_id;

-- name: ListPluginServerImagesFixture :many
SELECT ps.id, to_jsonb(ps)::text AS row_text
FROM plugin_servers AS ps
JOIN plugins AS pl ON pl.id = ps.plugin_id
WHERE pl.project_id = @project_id
ORDER BY ps.id;

-- name: SeedUserSessionIssuerFixture :exec
INSERT INTO user_session_issuers (id, project_id, organization_id, slug, authn_challenge_mode, session_duration)
VALUES (@id, @project_id, @organization_id, @slug, 'interactive', interval '1 hour');

-- name: SetServerIssuerFixture :exec
UPDATE mcp_servers SET user_session_issuer_id = @user_session_issuer_id
WHERE id = @id AND project_id = @project_id;

-- name: SeedGatewayFixture :exec
INSERT INTO meta_mcp_servers (id, organization_id, project_id, user_session_issuer_id, name)
VALUES (@id, @organization_id, @project_id, @user_session_issuer_id, @name);

-- name: SeedGatewayMemberFixture :exec
INSERT INTO meta_mcp_server_members (id, project_id, meta_mcp_server_id, mcp_server_id)
VALUES (@id, @project_id, @meta_mcp_server_id, @mcp_server_id);

-- name: SeedSessionClientFixture :exec
WITH issuer AS (
  INSERT INTO remote_session_issuers (project_id, slug, issuer)
  VALUES (@project_id::uuid, @slug::text, 'https://issuer.example.test/' || @slug::text)
  RETURNING id
)
INSERT INTO remote_session_clients (id, project_id, remote_session_issuer_id, client_id)
SELECT @id::uuid, @project_id::uuid, issuer.id, 'client-' || @slug::text
FROM issuer;

-- name: SeedSessionClientIssuerLinkFixture :exec
INSERT INTO remote_session_client_user_session_issuers (remote_session_client_id, user_session_issuer_id)
VALUES (@remote_session_client_id, @user_session_issuer_id);

-- name: SeedSessionClientLinkFixture :exec
INSERT INTO mcp_server_remote_session_clients (id, project_id, organization_id, mcp_server_id, remote_session_client_id, client_attachment_scope)
VALUES (@id, @project_id, @organization_id, @mcp_server_id, @remote_session_client_id, @client_attachment_scope);

-- name: SeedServerRiskPolicyFixture :exec
INSERT INTO risk_policies (id, project_id, organization_id, name, sources, version, mcp_scope)
VALUES (@id, @project_id, @organization_id, 'policy', ARRAY['fixture']::text[], 1,
  jsonb_build_object('all_servers', false, 'servers', jsonb_build_array(jsonb_build_object('mcp_server_id', @mcp_server_id::text))));

-- name: SeedSharedRiskPolicyFixture :exec
INSERT INTO risk_policies (id, project_id, organization_id, name, sources, version, mcp_scope)
VALUES (@id, @project_id, @organization_id, 'shared policy', ARRAY['fixture']::text[], 1,
  jsonb_build_object('servers', jsonb_build_array(
    jsonb_build_object('mcp_server_id', @first_server_id::text),
    jsonb_build_object('mcp_server_id', @second_server_id::text))));

-- name: GetRiskPolicyScopeFixture :one
SELECT mcp_scope::text AS scope, version FROM risk_policies WHERE id = @id AND project_id = @project_id;

-- name: TouchRiskPolicyFixture :exec
UPDATE risk_policies SET updated_at = clock_timestamp() WHERE id = @id AND project_id = @project_id;

-- name: ListWrapperStateFixture :many
-- Every row the wrapper delete can change in a project, as text.
SELECT 'mcp_servers'::text AS table_name, ms.id, to_jsonb(ms)::text AS row_text FROM mcp_servers AS ms WHERE ms.project_id = @project_id
UNION ALL
SELECT 'mcp_endpoints', e.id, to_jsonb(e)::text FROM mcp_endpoints AS e WHERE e.project_id = @project_id
UNION ALL
SELECT 'meta_mcp_server_members', m.id, to_jsonb(m)::text FROM meta_mcp_server_members AS m WHERE m.project_id = @project_id
UNION ALL
SELECT 'plugin_servers', ps.id, to_jsonb(ps)::text FROM plugin_servers AS ps JOIN plugins AS pl ON pl.id = ps.plugin_id WHERE pl.project_id = @project_id
UNION ALL
SELECT 'assistant_mcp_servers', a.id, to_jsonb(a)::text FROM assistant_mcp_servers AS a WHERE a.project_id = @project_id
UNION ALL
SELECT 'mcp_server_remote_session_clients', c.id, to_jsonb(c)::text FROM mcp_server_remote_session_clients AS c WHERE c.project_id = @project_id
UNION ALL
SELECT 'risk_policies', r.id, to_jsonb(r)::text FROM risk_policies AS r WHERE r.project_id = @project_id
ORDER BY 1, 2;

-- name: RestoreToolsetFixture :exec
UPDATE toolsets SET deleted_at = NULL
WHERE id = @id AND project_id = @project_id;

-- name: SetServerToolsetFixture :exec
UPDATE mcp_servers SET toolset_id = @toolset_id
WHERE id = @id AND project_id = @project_id;

-- name: DeleteSessionClientLinkFixture :exec
DELETE FROM mcp_server_remote_session_clients
WHERE id = @id AND project_id = @project_id;
