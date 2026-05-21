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

-- name: CheckSlugAvailability :one
-- Returns true when the slug is available for an mcp_endpoint in the given
-- uniqueness namespace. Platform-domain endpoints (custom_domain_id IS NULL)
-- and custom-domain endpoints live in separate namespaces enforced by partial
-- unique indexes; this query mirrors that scoping by treating NULL as a valid
-- match value via IS NOT DISTINCT FROM. Soft-deleted rows are ignored. The
-- slug-existence check is intentionally not project-scoped because the
-- uniqueness indexes it mirrors span all projects within their namespace.
--
-- When custom_domain_id is supplied, the domain must also belong to the
-- caller's organization. Foreign or unknown domains short-circuit to
-- "unavailable" (returns false) so callers can't probe slug-existence under
-- domains they don't own. organization_id is ignored on the platform-domain
-- branch (custom_domain_id IS NULL).
SELECT (
  sqlc.narg('custom_domain_id')::uuid IS NULL
  OR EXISTS (
    SELECT 1
    FROM custom_domains
    WHERE id = sqlc.narg('custom_domain_id')::uuid
      AND organization_id = @organization_id
      AND deleted IS FALSE
  )
) AND NOT EXISTS (
  SELECT 1
  FROM mcp_endpoints
  WHERE slug = @slug
    AND custom_domain_id IS NOT DISTINCT FROM sqlc.narg('custom_domain_id')::uuid
    AND deleted IS FALSE
);

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
