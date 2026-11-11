-- name: CreateMCPEndpoint :one
INSERT INTO mcp_endpoints (
    project_id,
    custom_domain_id,
    mcp_server_id,
    meta_mcp_server_id,
    slug
)
VALUES (
    @project_id,
    @custom_domain_id,
    @mcp_server_id,
    @meta_mcp_server_id,
    @slug
)
RETURNING *;

-- name: GetMCPEndpointByID :one
SELECT *
FROM mcp_endpoints
WHERE id = @id AND project_id = @project_id AND deleted IS FALSE;

-- name: LockMCPEndpointByID :one
SELECT *
FROM mcp_endpoints
WHERE id = @id AND project_id = @project_id AND deleted IS FALSE
FOR UPDATE;

-- name: GetMCPEndpointByProjectAndCustomDomainAndSlug :one
-- Resolve an endpoint by its (project_id, custom_domain_id, slug) triple.
-- This is intended for management use, to ensure the resolved endpoint belongs
-- to the correct project.
SELECT *
FROM mcp_endpoints
WHERE project_id = @project_id
  AND slug = @slug
  AND custom_domain_id IS NOT DISTINCT FROM @custom_domain_id
  AND deleted IS FALSE;

-- name: GetMCPEndpointByCustomDomainAndSlug :one
-- Resolve an endpoint by its globally-unique (custom_domain_id, slug) pair.
-- This is intended for use in the public-facing endpoint resolution path.
SELECT *
FROM mcp_endpoints
WHERE slug = @slug
  AND custom_domain_id IS NOT DISTINCT FROM @custom_domain_id
  AND deleted IS FALSE;

-- name: ListMCPEndpointsByProject :many
SELECT *
FROM mcp_endpoints
WHERE project_id = @project_id AND deleted IS FALSE
ORDER BY created_at DESC;

-- name: ListMCPEndpointsByMCPServerID :many
SELECT *
FROM mcp_endpoints
WHERE project_id = @project_id
  AND mcp_server_id = @mcp_server_id::uuid
  AND deleted IS FALSE
ORDER BY created_at DESC;

-- name: ListMCPEndpointsOnLiveDomainsByMCPServerID :many
-- Endpoints on a deleted or another organization's domain are skipped; those
-- on a domain still pending verification or activation are kept.
SELECT sqlc.embed(e)
FROM mcp_endpoints e
LEFT JOIN custom_domains cd
  ON cd.id = e.custom_domain_id
  AND cd.organization_id = @organization_id
  AND cd.deleted IS FALSE
WHERE e.project_id = @project_id
  AND e.mcp_server_id = @mcp_server_id::uuid
  AND e.deleted IS FALSE
  AND (e.custom_domain_id IS NULL OR cd.id IS NOT NULL);

-- name: ListServableMCPEndpointsByMCPServerID :many
-- Endpoints that can serve: on the platform, or on a live, verified and activated domain of the organization.
SELECT sqlc.embed(e)
FROM mcp_endpoints e
LEFT JOIN custom_domains cd
  ON cd.id = e.custom_domain_id
  AND cd.organization_id = @organization_id
  AND cd.deleted IS FALSE
  AND cd.verified
  AND cd.activated
WHERE e.project_id = @project_id
  AND e.mcp_server_id = @mcp_server_id::uuid
  AND e.deleted IS FALSE
  AND (e.custom_domain_id IS NULL OR cd.id IS NOT NULL);

-- name: ListMCPEndpointsByMetaMCPServerID :many
SELECT *
FROM mcp_endpoints
WHERE project_id = @project_id
  AND meta_mcp_server_id = @meta_mcp_server_id::uuid
  AND deleted IS FALSE
ORDER BY created_at DESC;

-- name: ListMCPEndpointsByCustomDomainID :many
-- List active endpoints (across every project under the owning org) registered
-- under a custom domain, with the parent server name/slug and project
-- name/slug joined in. Used by the org-scoped domains.listMcpEndpoints handler
-- to preview the impact of a custom domain deletion. The server name comes
-- from whichever backend the endpoint addresses; only generic servers carry a
-- slug (meta MCP servers have none).
SELECT
    e.id,
    e.project_id,
    e.mcp_server_id,
    e.meta_mcp_server_id,
    e.slug,
    e.is_domain_root,
    p.name AS project_name,
    p.slug AS project_slug,
    COALESCE(s.name, ms.name, '') AS mcp_server_name,
    s.slug AS mcp_server_slug
FROM mcp_endpoints e
JOIN projects p ON p.id = e.project_id
LEFT JOIN mcp_servers s ON s.id = e.mcp_server_id
LEFT JOIN meta_mcp_servers ms ON ms.id = e.meta_mcp_server_id
WHERE e.custom_domain_id = @custom_domain_id::uuid
  AND e.deleted IS FALSE
ORDER BY p.slug, e.slug;

-- name: UpdateMCPEndpointAddress :one
UPDATE mcp_endpoints
SET
    custom_domain_id = @custom_domain_id,
    slug = @slug,
    is_domain_root = @is_domain_root,
    updated_at = clock_timestamp()
WHERE id = @id AND project_id = @project_id AND deleted IS FALSE
RETURNING *;

-- name: UpdateMCPEndpoint :one
UPDATE mcp_endpoints
SET
    custom_domain_id = @custom_domain_id,
    mcp_server_id = @mcp_server_id,
    meta_mcp_server_id = @meta_mcp_server_id,
    slug = @slug,
    is_domain_root = @is_domain_root,
    updated_at = clock_timestamp()
WHERE id = @id AND project_id = @project_id AND deleted IS FALSE
RETURNING *;

-- name: DeleteMCPEndpoint :one
UPDATE mcp_endpoints
SET
    is_domain_root = NULL,
    deleted_at = clock_timestamp()
WHERE id = @id AND project_id = @project_id AND deleted IS FALSE
RETURNING *;

-- name: CheckEndpointSlugAvailability :one
-- True when no live mcp_endpoints.slug holds the slug in the namespace
-- (platform when custom_domain_id is NULL, else that domain).
-- Not project-scoped, mirroring the partial unique index. Unless
-- skip_domain_check, a supplied domain must be live and owned by
-- organization_id or the result is false (blocks probing foreign domains).
SELECT (
  @skip_domain_check::boolean
  OR sqlc.narg('custom_domain_id')::uuid IS NULL
  OR EXISTS (
    SELECT 1
    FROM custom_domains cd
    WHERE cd.id = sqlc.narg('custom_domain_id')::uuid
      AND cd.organization_id = @organization_id
      AND cd.deleted IS FALSE
  )
) AND NOT EXISTS (
  SELECT 1
  FROM mcp_endpoints e
  WHERE e.slug = @slug
    AND e.custom_domain_id IS NOT DISTINCT FROM sqlc.narg('custom_domain_id')::uuid
    AND e.deleted IS FALSE
);

-- name: LockSlugScope :exec
-- Serializes competing claims on one (namespace, slug) address for the rest of
-- the caller's transaction, since the availability check runs before the write.
-- A custom-domain claim also takes the platform lock first: it no longer needs
-- it, but keeping one lock order stays deadlock-free beside pods still running
-- the earlier order.
SELECT
  pg_advisory_xact_lock(hashtextextended('mcp_slug:platform/' || @slug::text, 0)),
  pg_advisory_xact_lock(hashtextextended(
    'mcp_slug:' || coalesce(sqlc.narg('custom_domain_id')::uuid::text, 'platform') || '/' || @slug::text, 0
  ));

-- name: TryLockPlatformSlugScope :one
-- LockSlugScope's platform lock without waiting, for a write that already holds another slug's lock.
SELECT pg_try_advisory_xact_lock(hashtextextended('mcp_slug:platform/' || @slug::text, 0))::boolean AS locked;

-- name: SoftDeleteMCPEndpointsByMCPServerID :many
-- Soft-delete all endpoints that point at a given mcp server. Used when the
-- parent server is soft-deleted so callers don't end up with endpoints pointing
-- at a tombstoned server (the FK ON DELETE CASCADE does not fire for soft
-- deletes). Returns the affected rows so the caller can emit per-endpoint
-- audit events for the cascade.
UPDATE mcp_endpoints
SET
    is_domain_root = NULL,
    deleted_at = clock_timestamp()
WHERE mcp_server_id = @mcp_server_id::uuid AND project_id = @project_id AND deleted IS FALSE
RETURNING *;

-- name: SoftDeleteMCPEndpointsByMetaMCPServerID :many
-- Soft-delete all endpoints that point at a given meta MCP server. Used when
-- the parent meta MCP server is soft-deleted so callers don't end up with
-- endpoints pointing at a tombstoned server (the FK ON DELETE CASCADE does not
-- fire for soft deletes). Returns the affected rows so the caller can emit
-- per-endpoint audit events for the cascade.
UPDATE mcp_endpoints
SET
    is_domain_root = NULL,
    deleted_at = clock_timestamp()
WHERE meta_mcp_server_id = @meta_mcp_server_id::uuid AND project_id = @project_id AND deleted IS FALSE
RETURNING *;

-- name: SoftDeleteMCPEndpointsByCustomDomainID :many
-- Soft-delete all endpoints registered under a given custom_domain. Used when
-- the parent custom_domain is soft-deleted so the endpoints don't outlive the
-- domain (the FK ON DELETE SET NULL does not fire for soft deletes). Returns
-- the affected rows so the caller can emit per-endpoint audit events. Scoped
-- by custom_domain_id alone (no project_id): custom_domains are org-scoped and
-- the cascade legitimately crosses every project under the owning org. The
-- caller must verify the custom_domain belongs to its organization before
-- invoking this query.
UPDATE mcp_endpoints
SET
    is_domain_root = NULL,
    deleted_at = clock_timestamp()
WHERE custom_domain_id = @custom_domain_id::uuid AND deleted IS FALSE
RETURNING *;

-- name: ListRootMCPEndpointsByMCPServerID :many
SELECT *
FROM mcp_endpoints
WHERE mcp_server_id = @mcp_server_id::uuid
  AND project_id = @project_id
  AND is_domain_root IS TRUE
  AND deleted IS FALSE
ORDER BY custom_domain_id, id;

-- name: ListCustomDomainIDsByMCPServerID :many
SELECT DISTINCT custom_domain_id::uuid
FROM mcp_endpoints
WHERE mcp_server_id = @mcp_server_id::uuid
  AND project_id = @project_id
  AND custom_domain_id IS NOT NULL
  AND deleted IS FALSE
ORDER BY custom_domain_id::uuid;

-- name: ListCustomDomainIDsByMetaMCPServerID :many
SELECT DISTINCT custom_domain_id::uuid
FROM mcp_endpoints
WHERE meta_mcp_server_id = @meta_mcp_server_id::uuid
  AND project_id = @project_id
  AND custom_domain_id IS NOT NULL
  AND deleted IS FALSE
ORDER BY custom_domain_id::uuid;

-- name: LockRootMCPEndpointsByMCPServerID :many
SELECT *
FROM mcp_endpoints
WHERE mcp_server_id = @mcp_server_id::uuid
  AND project_id = @project_id
  AND is_domain_root IS TRUE
  AND deleted IS FALSE
ORDER BY id
FOR UPDATE;

-- name: LockMCPEndpointsByMCPServerID :many
-- Lock every live endpoint (not only current roots) before the server row
-- lock: root selection holds endpoint locks while waiting on the server row,
-- so writing an unlocked endpoint after taking the server lock can deadlock.
-- Re-run after the server lock for the authoritative pre-delete root set.
SELECT *
FROM mcp_endpoints
WHERE mcp_server_id = @mcp_server_id::uuid
  AND project_id = @project_id
  AND deleted IS FALSE
ORDER BY id
FOR UPDATE;

-- name: LockMCPEndpointsByMetaMCPServerID :many
-- Lock every live endpoint (not only current roots) before the meta server
-- row lock: endpoint mutations hold endpoint locks while waiting on the meta
-- row, so writing an unlocked endpoint after taking the meta lock can
-- deadlock. Re-run after the meta lock for the authoritative pre-delete root
-- set.
SELECT *
FROM mcp_endpoints
WHERE meta_mcp_server_id = @meta_mcp_server_id::uuid
  AND project_id = @project_id
  AND deleted IS FALSE
ORDER BY id
FOR UPDATE;

-- name: ClearRootMCPEndpointsByMCPServerID :many
UPDATE mcp_endpoints
SET
    is_domain_root = NULL,
    updated_at = clock_timestamp()
WHERE mcp_server_id = @mcp_server_id::uuid
  AND project_id = @project_id
  AND is_domain_root IS TRUE
  AND deleted IS FALSE
RETURNING *;

-- name: ListAddressableMCPEndpointsByMCPServerIDs :many
-- Custom-domain endpoints count only on a verified, activated domain, the
-- same gate the custom-domain middleware applies before serving. Mirrored in
-- assistants LoadAssistantMcpServers and ResolveMcpServersForWrite.
SELECT sqlc.embed(e), cd.domain AS custom_domain
FROM mcp_endpoints e
JOIN projects p
  ON p.id = e.project_id
  AND p.organization_id = @organization_id
LEFT JOIN custom_domains cd
  ON cd.id = e.custom_domain_id
  AND cd.organization_id = @organization_id
  AND cd.deleted IS FALSE
  AND cd.verified IS TRUE
  AND cd.activated IS TRUE
WHERE e.project_id = ANY(@project_ids::uuid[])
  AND e.mcp_server_id = ANY(@mcp_server_ids::uuid[])
  AND e.deleted IS FALSE
  AND (e.custom_domain_id IS NULL OR cd.id IS NOT NULL);

-- name: ListMCPEndpointsByProjectAndSlug :many
-- Live endpoints with a slug in the project, platform addresses first.
SELECT *
FROM mcp_endpoints
WHERE project_id = @project_id
  AND slug = @slug
  AND deleted IS FALSE
ORDER BY (custom_domain_id IS NULL) DESC, created_at, id;
