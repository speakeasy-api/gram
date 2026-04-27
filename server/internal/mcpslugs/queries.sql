-- name: CreateMCPSlug :one
INSERT INTO mcp_slugs (
    project_id,
    custom_domain_id,
    mcp_frontend_id,
    slug
)
VALUES (
    @project_id,
    @custom_domain_id,
    @mcp_frontend_id,
    @slug
)
RETURNING *;

-- name: GetMCPSlugByID :one
SELECT *
FROM mcp_slugs
WHERE id = @id AND project_id = @project_id AND deleted IS FALSE;

-- name: GetMCPSlugByCustomDomainIDAndSlug :one
SELECT *
FROM mcp_slugs
WHERE project_id = @project_id
  AND slug = @slug
  AND custom_domain_id IS NOT DISTINCT FROM @custom_domain_id
  AND deleted IS FALSE;

-- name: ListMCPSlugsByProject :many
SELECT *
FROM mcp_slugs
WHERE project_id = @project_id AND deleted IS FALSE
ORDER BY created_at DESC;

-- name: ListMCPSlugsByFrontendID :many
SELECT *
FROM mcp_slugs
WHERE project_id = @project_id
  AND mcp_frontend_id = @mcp_frontend_id
  AND deleted IS FALSE
ORDER BY created_at DESC;

-- name: UpdateMCPSlug :one
UPDATE mcp_slugs
SET
    custom_domain_id = @custom_domain_id,
    mcp_frontend_id = @mcp_frontend_id,
    slug = @slug,
    updated_at = clock_timestamp()
WHERE id = @id AND project_id = @project_id AND deleted IS FALSE
RETURNING *;

-- name: DeleteMCPSlug :one
UPDATE mcp_slugs
SET deleted_at = clock_timestamp()
WHERE id = @id AND project_id = @project_id AND deleted IS FALSE
RETURNING *;

-- name: SoftDeleteMCPSlugsByFrontendID :exec
-- Soft-delete all slugs that point at a given frontend. Used when the parent
-- frontend is soft-deleted so callers don't end up with slugs pointing at a
-- tombstoned frontend (the FK ON DELETE CASCADE does not fire for soft deletes).
UPDATE mcp_slugs
SET deleted_at = clock_timestamp()
WHERE mcp_frontend_id = @mcp_frontend_id AND project_id = @project_id AND deleted IS FALSE;
