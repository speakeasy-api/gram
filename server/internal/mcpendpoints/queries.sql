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

-- name: CheckUnifiedSlugAvailability :one
-- True when no live mcp_endpoints.slug or toolsets.mcp_slug holds the slug in
-- the namespace (platform when custom_domain_id is NULL, else that domain).
-- Not project-scoped, mirroring the partial unique indexes. Owner exclusions:
-- exclude_toolset_id discounts that toolset's row and its wrapper's endpoints;
-- exclude_mcp_server_id discounts the toolset backing that server. Unless
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
    AND (
      sqlc.narg('exclude_toolset_id')::uuid IS NULL
      OR NOT EXISTS (
        SELECT 1
        FROM mcp_servers s
        WHERE s.id = e.mcp_server_id
          AND s.toolset_id = sqlc.narg('exclude_toolset_id')::uuid
      )
    )
) AND NOT EXISTS (
  SELECT 1
  FROM toolsets t
  WHERE t.mcp_slug = @slug
    AND t.custom_domain_id IS NOT DISTINCT FROM sqlc.narg('custom_domain_id')::uuid
    AND t.deleted IS FALSE
    AND (
      sqlc.narg('exclude_toolset_id')::uuid IS NULL
      OR t.id <> sqlc.narg('exclude_toolset_id')::uuid
    )
    AND (
      sqlc.narg('exclude_mcp_server_id')::uuid IS NULL
      OR NOT EXISTS (
        SELECT 1
        FROM mcp_servers s
        WHERE s.id = sqlc.narg('exclude_mcp_server_id')::uuid
          AND s.toolset_id = t.id
      )
    )
);

-- name: LockSlugScope :exec
-- Serializes competing claims on one (namespace, slug) address for the rest of
-- the caller's transaction; the per-table unique indexes cannot see
-- cross-table collisions.
SELECT pg_advisory_xact_lock(hashtextextended(
  'mcp_slug:' || coalesce(sqlc.narg('custom_domain_id')::uuid::text, 'platform') || '/' || @slug::text, 0
));

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
