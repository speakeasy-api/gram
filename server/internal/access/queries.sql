-- Queries for managing principal grants (RBAC).
-- principal_grants is org-scoped (no project_id); every query is scoped to organization_id.

-- name: ListPrincipalGrantsByOrg :many
-- Returns all grant rows for an organization, optionally filtered by principal URN.
SELECT id, organization_id, principal_urn, principal_type, scope, resource, created_at, updated_at
FROM principal_grants
WHERE organization_id = @organization_id
  AND (@principal_urn::text = '' OR principal_urn = @principal_urn)
ORDER BY principal_urn, scope, resource;

-- name: GetPrincipalGrants :many
-- Returns all grant rows matching a set of principal URNs within an org.
-- Used by the access resolver to load grants for a user+role in a single query.
SELECT scope, resource
FROM principal_grants
WHERE organization_id = @organization_id
  AND principal_urn = ANY(@principal_urns::text[]);

-- name: UpsertPrincipalGrant :one
-- Creates or updates a single grant row. On conflict (same org/principal/scope/resource),
-- the updated_at timestamp is refreshed. Returns the full row.
INSERT INTO principal_grants (organization_id, principal_urn, scope, resource)
VALUES (@organization_id, @principal_urn, @scope, @resource)
ON CONFLICT (organization_id, principal_urn, scope, resource)
DO UPDATE SET updated_at = clock_timestamp()
RETURNING id, organization_id, principal_urn, principal_type, scope, resource, created_at, updated_at;

-- name: DeletePrincipalGrant :execrows
-- Removes a specific grant row by ID, scoped to the organization for safety.
DELETE FROM principal_grants
WHERE id = @id
  AND organization_id = @organization_id;

-- name: DeletePrincipalGrantByTuple :execrows
-- Removes a single grant row matching the exact (org, principal, scope, resource) tuple.
DELETE FROM principal_grants
WHERE organization_id = @organization_id
  AND principal_urn = @principal_urn
  AND scope = @scope
  AND resource = @resource;

-- name: DeletePrincipalGrantsByPrincipal :execrows
-- Removes all grants for a specific principal within an org.
-- Useful when removing a user from an organization.
DELETE FROM principal_grants
WHERE organization_id = @organization_id
  AND principal_urn = @principal_urn;

-- name: RemoveResourceFromGrants :execrows
-- Deletes all grant rows referencing a specific resource within an org.
-- Called when a resource (project, MCP server) is deleted.
DELETE FROM principal_grants
WHERE organization_id = @organization_id
  AND resource = @resource;

-- name: UpsertRole :exec
INSERT INTO organization_roles (
    organization_id,
    workos_id,
    workos_slug,
    workos_name,
    workos_description,
    workos_created_at,
    workos_updated_at,
    workos_last_event_id
)
VALUES (
    @organization_id,
    @workos_id,
    @workos_slug,
    @workos_name,
    @workos_description,
    @workos_created_at,
    @workos_updated_at,
    @workos_last_event_id
)
ON CONFLICT (organization_id, workos_id)
DO UPDATE SET
  workos_slug = EXCLUDED.workos_slug,
  workos_name = EXCLUDED.workos_name,
  workos_description = EXCLUDED.workos_description,
  workos_created_at = EXCLUDED.workos_created_at,
  workos_updated_at = EXCLUDED.workos_updated_at,
  workos_deleted_at = NULL,
  workos_last_event_id = EXCLUDED.workos_last_event_id,
  updated_at = clock_timestamp()
  WHERE organization_roles.workos_updated_at < EXCLUDED.workos_updated_at;

-- name: MarkRoleDeleted :execrows
UPDATE organization_roles
SET workos_deleted_at = @workos_deleted_at,
    workos_last_event_id = @workos_last_event_id,
    updated_at = clock_timestamp()
WHERE organization_id = @organization_id
  AND workos_id = @workos_id
  AND (
    workos_deleted_at IS NULL
    OR workos_deleted_at < @workos_deleted_at
  );
