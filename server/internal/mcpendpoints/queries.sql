-- name: CreateMCPEndpoint :one
INSERT INTO mcp_endpoints (
    project_id,
    custom_domain_id,
    mcp_server_id,
    slug
)
VALUES (
    @project_id,
    @custom_domain_id,
    @mcp_server_id,
    @slug
)
RETURNING *;

-- name: GetMCPEndpointByID :one
SELECT *
FROM mcp_endpoints
WHERE id = @id AND project_id = @project_id AND deleted IS FALSE;

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
  AND mcp_server_id = @mcp_server_id
  AND deleted IS FALSE
ORDER BY created_at DESC;

-- name: ListMCPEndpointsByCustomDomainID :many
-- List active endpoints (across every project under the owning org) registered
-- under a custom domain, with the parent mcp_server name/slug and project
-- name/slug joined in. Used by the org-scoped domains.listMcpEndpoints handler
-- to preview the impact of a custom domain deletion.
SELECT
    e.id,
    e.project_id,
    e.mcp_server_id,
    e.slug,
    p.name AS project_name,
    p.slug AS project_slug,
    s.name AS mcp_server_name,
    s.slug AS mcp_server_slug
FROM mcp_endpoints e
JOIN projects p ON p.id = e.project_id
JOIN mcp_servers s ON s.id = e.mcp_server_id
WHERE e.custom_domain_id = @custom_domain_id::uuid
  AND e.deleted IS FALSE
ORDER BY p.slug, e.slug;

-- name: UpdateMCPEndpoint :one
UPDATE mcp_endpoints
SET
    custom_domain_id = @custom_domain_id,
    mcp_server_id = @mcp_server_id,
    slug = @slug,
    updated_at = clock_timestamp()
WHERE id = @id AND project_id = @project_id AND deleted IS FALSE
RETURNING *;

-- name: DeleteMCPEndpoint :one
UPDATE mcp_endpoints
SET deleted_at = clock_timestamp()
WHERE id = @id AND project_id = @project_id AND deleted IS FALSE
RETURNING *;

-- name: SoftDeleteMCPEndpointsByMCPServerID :many
-- Soft-delete all endpoints that point at a given mcp server. Used when the
-- parent server is soft-deleted so callers don't end up with endpoints pointing
-- at a tombstoned server (the FK ON DELETE CASCADE does not fire for soft
-- deletes). Returns the affected rows so the caller can emit per-endpoint
-- audit events for the cascade.
UPDATE mcp_endpoints
SET deleted_at = clock_timestamp()
WHERE mcp_server_id = @mcp_server_id AND project_id = @project_id AND deleted IS FALSE
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
SET deleted_at = clock_timestamp()
WHERE custom_domain_id = @custom_domain_id::uuid AND deleted IS FALSE
RETURNING *;
