-- Queries backing the wraptoolsets admin migration command: wrapping every
-- live toolset that still publishes directly through toolsets.mcp_slug in a
-- toolset-backed mcp_servers row plus one mcp_endpoints row, and moving
-- dependent mcp_metadata and collection attachment ownership onto the wrapper.

-- name: ListCandidateToolsets :many
SELECT t.id, t.project_id
FROM toolsets t
INNER JOIN projects p ON p.id = t.project_id AND p.deleted IS FALSE
WHERE t.deleted IS FALSE
  AND t.mcp_slug IS NOT NULL
  AND t.id > @after_id::uuid
  AND (sqlc.narg('project_id')::uuid IS NULL OR t.project_id = sqlc.narg('project_id')::uuid)
ORDER BY t.id
LIMIT NULLIF(@row_limit::bigint, 0);

-- name: AcquireWrapRunLock :exec
SELECT pg_advisory_xact_lock(@lock_key::bigint);

-- name: LockCandidateToolset :one
SELECT id, organization_id, project_id, name, slug, default_environment_slug,
       mcp_slug, mcp_is_public, mcp_enabled, custom_domain_id
FROM toolsets
WHERE id = @id
  AND project_id = @project_id
  AND deleted IS FALSE
  AND mcp_slug IS NOT NULL
FOR UPDATE;

-- name: GetLiveProject :one
SELECT id, organization_id
FROM projects
WHERE id = @id AND deleted IS FALSE;

-- name: GetCustomDomainState :one
-- Soft-deleted rows are returned on purpose: a candidate whose domain is
-- tombstoned needs the blocked_dead_domain / -clear-dead-domain handling.
SELECT id, organization_id, deleted
FROM custom_domains
WHERE id = @id;

-- name: ResolveDefaultEnvironment :one
SELECT id
FROM environments
WHERE project_id = @project_id AND slug = @slug AND deleted IS FALSE;

-- name: ListLiveToolsetWrappers :many
SELECT id, project_id, name, slug, environment_id, user_session_issuer_id,
       remote_mcp_server_id, tunneled_mcp_server_id, toolset_id,
       tool_variations_group_id, visibility
FROM mcp_servers
WHERE toolset_id = @toolset_id AND project_id = @project_id AND deleted IS FALSE
ORDER BY id;

-- name: FindLiveEndpointAtAddress :many
-- Deliberately not project-scoped: endpoint slugs are a global namespace (per
-- custom domain, or platform-wide when custom_domain_id is NULL), so an
-- occupant in any project must block the candidate.
SELECT e.id, e.project_id, e.mcp_server_id, e.slug, e.custom_domain_id
FROM mcp_endpoints e
WHERE e.deleted IS FALSE
  AND e.slug = @slug
  AND e.custom_domain_id IS NOT DISTINCT FROM sqlc.narg('custom_domain_id')::uuid;

-- name: GetMcpServerRow :one
-- Deliberately not project-scoped: a row sitting at the derived id in any
-- project is a conflict the command must surface rather than miss.
SELECT id, project_id, name, slug, environment_id, user_session_issuer_id,
       remote_mcp_server_id, tunneled_mcp_server_id, toolset_id,
       tool_variations_group_id, visibility, created_at, updated_at, deleted
FROM mcp_servers
WHERE id = @id;

-- name: GetMcpEndpointRow :one
-- Same global scoping rationale as GetMcpServerRow.
SELECT id, project_id, custom_domain_id, mcp_server_id, slug, is_domain_root,
       created_at, updated_at, deleted
FROM mcp_endpoints
WHERE id = @id;

-- name: FindLiveServerSlugOwner :one
SELECT id, toolset_id
FROM mcp_servers
WHERE project_id = @project_id AND slug = @slug AND deleted IS FALSE;

-- name: ClearToolsetCustomDomain :execrows
UPDATE toolsets
SET custom_domain_id = NULL, updated_at = clock_timestamp()
WHERE id = @id AND project_id = @project_id AND deleted IS FALSE;

-- name: InsertWrapperMcpServer :one
INSERT INTO mcp_servers (
    id
  , project_id
  , name
  , slug
  , environment_id
  , user_session_issuer_id
  , remote_mcp_server_id
  , tunneled_mcp_server_id
  , toolset_id
  , tool_variations_group_id
  , visibility
) VALUES (
    @id
  , @project_id
  , @name
  , @slug
  , @environment_id
  , NULL
  , NULL
  , NULL
  , @toolset_id
  , NULL
  , @visibility
)
RETURNING id;

-- name: InsertWrapperMcpEndpoint :one
-- Repo-layer write on purpose: the slug is copied verbatim from
-- toolsets.mcp_slug and must bypass the service-layer org-prefix validation.
INSERT INTO mcp_endpoints (
    id
  , project_id
  , custom_domain_id
  , mcp_server_id
  , slug
  , is_domain_root
) VALUES (
    @id
  , @project_id
  , @custom_domain_id
  , @mcp_server_id
  , @slug
  , NULL
)
RETURNING id;

-- name: ListToolsetOwnedMetadata :many
-- Deliberately not project-scoped: a metadata row owned by the toolset but
-- carrying a different project_id is a tenancy anomaly the command must block
-- on instead of silently leaving behind.
SELECT id, project_id
FROM mcp_metadata
WHERE toolset_id = @toolset_id;

-- name: ListServerOwnedMetadata :many
SELECT id
FROM mcp_metadata
WHERE mcp_server_id = @mcp_server_id AND project_id = @project_id;

-- name: MoveMetadataOwnershipToServer :execrows
-- Owner-column move only, so the metadata id, timestamps and its
-- mcp_environment_configs children are preserved.
UPDATE mcp_metadata
SET toolset_id = NULL, mcp_server_id = @mcp_server_id
WHERE toolset_id = @toolset_id AND project_id = @project_id;

-- name: CountToolsetOwnedCollectionAttachments :one
SELECT count(*)
FROM organization_mcp_collection_server_attachments
WHERE toolset_id = @toolset_id;

-- name: CountConflictingCollectionAttachments :one
SELECT count(*)
FROM organization_mcp_collection_server_attachments server_side
WHERE server_side.deleted IS FALSE
  AND server_side.mcp_server_id = @mcp_server_id
  AND server_side.collection_id IN (
    SELECT toolset_side.collection_id
    FROM organization_mcp_collection_server_attachments toolset_side
    WHERE toolset_side.toolset_id = @toolset_id AND toolset_side.deleted IS FALSE
  );

-- name: MoveCollectionAttachmentOwnershipToServer :execrows
-- Moves soft-deleted history rows too, preserving ids, timestamps,
-- published_by and deleted_at; only the owner columns change. The table has
-- no project_id column, so tenancy is anchored on the locked toolset row.
UPDATE organization_mcp_collection_server_attachments
SET toolset_id = NULL, mcp_server_id = @mcp_server_id
WHERE toolset_id = @toolset_id;

-- Queries for the -move-plugins mode: rekeying plugin_servers rows from
-- toolset_id onto the toolset's live wrapper mcp_server.

-- name: ListPluginMoveCandidateToolsets :many
-- Candidates are live toolsets in live projects that still have any
-- plugin_servers row (live or soft-deleted history) keyed by toolset_id.
-- After a successful move a toolset drops out of this scan, which is what
-- makes reruns naturally idempotent.
SELECT DISTINCT t.id, t.project_id
FROM toolsets t
INNER JOIN projects p ON p.id = t.project_id AND p.deleted IS FALSE
INNER JOIN plugin_servers ps ON ps.toolset_id = t.id
WHERE t.deleted IS FALSE
  AND t.id > @after_id::uuid
  AND (sqlc.narg('project_id')::uuid IS NULL OR t.project_id = sqlc.narg('project_id')::uuid)
ORDER BY t.id
LIMIT NULLIF(@row_limit::bigint, 0);

-- name: LockPluginMoveToolset :one
-- Unlike LockCandidateToolset there is no mcp_slug requirement: the wrapper
-- (not the toolset publishing columns) anchors the plugin move.
SELECT id, organization_id, project_id, name, slug
FROM toolsets
WHERE id = @id
  AND project_id = @project_id
  AND deleted IS FALSE
FOR UPDATE;

-- name: CountConflictingPluginAttachments :one
-- Plugins where a live toolset-keyed row and a live wrapper-keyed row would
-- collide after the move (the partial unique index on
-- (plugin_id, mcp_server_id) covers live rows only).
SELECT count(*)
FROM plugin_servers wrapper_side
WHERE wrapper_side.deleted IS FALSE
  AND wrapper_side.mcp_server_id = @mcp_server_id
  AND wrapper_side.plugin_id IN (
    SELECT toolset_side.plugin_id
    FROM plugin_servers toolset_side
    WHERE toolset_side.toolset_id = @toolset_id AND toolset_side.deleted IS FALSE
  );

-- name: CountToolsetKeyedPluginServers :one
-- Live and soft-deleted rows both count: history moves too.
SELECT count(*)
FROM plugin_servers
WHERE toolset_id = @toolset_id;

-- name: MovePluginServerOwnershipToServer :execrows
-- Moves soft-deleted history rows too, preserving ids, display_name, policy,
-- sort_order, timestamps, and deleted_at; only the owner columns change. The
-- table has no project_id column, so tenancy is anchored on the locked
-- toolset row and the project-scoped wrapper lookup.
UPDATE plugin_servers
SET toolset_id = NULL, mcp_server_id = @mcp_server_id
WHERE toolset_id = @toolset_id;

-- Test fixtures and verification reads.

-- name: InsertPreSwapToolsetFixture :one
-- Seeds a toolset carrying pre-swap publishing columns. The application
-- codebase no longer writes these columns; this command's tests must keep
-- exercising rows that predate the mcp_servers swap, so the fixture lives
-- here with the command's other raw-column queries.
INSERT INTO toolsets (
    organization_id
  , project_id
  , name
  , slug
  , description
  , default_environment_slug
  , mcp_slug
  , mcp_enabled
  , mcp_is_public
  , custom_domain_id
) VALUES (
    @organization_id
  , @project_id
  , @name
  , @slug
  , @description
  , @default_environment_slug
  , @mcp_slug
  , @mcp_enabled
  , @mcp_is_public
  , @custom_domain_id
)
RETURNING *;

-- name: InsertPluginFixture :one
INSERT INTO plugins (organization_id, project_id, name, slug)
VALUES (@organization_id, @project_id, @name, @slug)
RETURNING id;

-- name: InsertPluginServerFixture :one
INSERT INTO plugin_servers (plugin_id, toolset_id, mcp_server_id, display_name, policy, sort_order, deleted_at)
VALUES (
    @plugin_id
  , sqlc.narg('toolset_id')::uuid
  , sqlc.narg('mcp_server_id')::uuid
  , @display_name
  , @policy
  , @sort_order
  , sqlc.narg('deleted_at')::timestamptz
)
RETURNING *;

-- name: ListPluginServerRowsByPluginID :many
SELECT id, plugin_id, toolset_id, mcp_server_id, display_name, policy,
       sort_order, created_at, updated_at, deleted_at
FROM plugin_servers
WHERE plugin_id = @plugin_id
ORDER BY id;

-- name: GetToolsetRow :one
SELECT id, project_id, custom_domain_id, mcp_slug, updated_at
FROM toolsets
WHERE id = @id AND project_id = @project_id;

-- name: CountMcpServersInProject :one
SELECT count(*) FROM mcp_servers WHERE project_id = @project_id;

-- name: CountMcpEndpointsInProject :one
SELECT count(*) FROM mcp_endpoints WHERE project_id = @project_id;

-- name: GetMetadataRow :one
SELECT id, toolset_id, mcp_server_id, project_id, created_at, updated_at
FROM mcp_metadata
WHERE id = @id AND project_id = @project_id;

-- name: ListCollectionAttachmentRows :many
SELECT id, collection_id, toolset_id, mcp_server_id, published_at,
       published_by, created_at, updated_at, deleted_at
FROM organization_mcp_collection_server_attachments
WHERE collection_id = @collection_id
ORDER BY id;
