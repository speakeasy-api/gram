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
