-- Queries for managing principal grants (RBAC).
-- principal_grants is org-scoped (no project_id); every query is scoped to organization_id.

-- name: ListPrincipalGrantsByOrg :many
-- Returns all grant rows for an organization, optionally filtered by principal URN.
SELECT id, organization_id, principal_urn, principal_type, scope, selector, created_at, updated_at
FROM principal_grants
WHERE organization_id = @organization_id
  AND (@principal_urn::text = '' OR principal_urn = @principal_urn)
ORDER BY principal_urn, scope, selector;

-- name: GetPrincipalGrants :many
-- Returns all grant rows matching a set of principal URNs within an org.
-- Used by the access resolver to load grants for a user+role in a single query.
SELECT scope, selector
FROM principal_grants
WHERE organization_id = @organization_id
  AND principal_urn = ANY(@principal_urns::text[]);

-- name: UpsertPrincipalGrant :one
-- Creates or updates a single grant row. On conflict (same org/principal/scope/selector),
-- the updated_at timestamp is refreshed. Returns the full row.
INSERT INTO principal_grants (organization_id, principal_urn, scope, selector)
VALUES (@organization_id, @principal_urn, @scope, @selector)
ON CONFLICT (organization_id, principal_urn, scope, selector)
DO UPDATE SET updated_at = clock_timestamp()
RETURNING id, organization_id, principal_urn, principal_type, scope, selector, created_at, updated_at;

-- name: DeletePrincipalGrant :execrows
-- Removes a specific grant row by ID, scoped to the organization for safety.
DELETE FROM principal_grants
WHERE id = @id
  AND organization_id = @organization_id;

-- name: DeletePrincipalGrantByTuple :execrows
-- Removes a single grant row matching the exact (org, principal, scope, selector) tuple.
DELETE FROM principal_grants
WHERE organization_id = @organization_id
  AND principal_urn = @principal_urn
  AND scope = @scope
  AND selector = @selector;

-- name: DeletePrincipalGrantsByPrincipal :execrows
-- Removes all grants for a specific principal within an org.
-- Useful when removing a user from an organization.
DELETE FROM principal_grants
WHERE organization_id = @organization_id
  AND principal_urn = @principal_urn;

-- name: RemoveMatchingGrants :execrows
-- Deletes all grant rows whose selector contains the given selector within an org.
-- Called when a resource (project, MCP server) is deleted.
DELETE FROM principal_grants
WHERE organization_id = @organization_id
  AND selector @> @selector::jsonb;
