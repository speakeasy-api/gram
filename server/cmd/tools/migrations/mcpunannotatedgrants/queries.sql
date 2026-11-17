-- name: ListDispositionLimitedPrincipals :many
-- Principals holding an MCP connect rule or block limited to a disposition
-- other than unannotated.
SELECT grants.organization_id, grants.principal_urn
FROM principal_grants AS grants
WHERE grants.scope = ANY(@scopes::text[])
  AND grants.selectors ? 'disposition'
  AND grants.selectors ->> 'disposition' <> @unannotated::text
  AND (grants.expires_at IS NULL OR grants.expires_at > clock_timestamp())
  AND (sqlc.narg('organization_id')::text IS NULL OR grants.organization_id = sqlc.narg('organization_id')::text)
GROUP BY grants.organization_id, grants.principal_urn
ORDER BY grants.organization_id, grants.principal_urn;

-- name: LockDispositionLimitedGrants :many
-- The disposition-limited connect rules and blocks of one principal, locked
-- until the transaction ends.
SELECT grants.id, grants.scope, grants.selectors, grants.expires_at
FROM principal_grants AS grants
WHERE grants.organization_id = @organization_id
  AND grants.principal_urn = @principal_urn
  AND grants.scope = ANY(@scopes::text[])
  AND grants.selectors ? 'disposition'
  AND grants.selectors ->> 'disposition' <> @unannotated::text
  AND (grants.expires_at IS NULL OR grants.expires_at > clock_timestamp())
ORDER BY grants.id
FOR UPDATE;

-- name: LockGrantBySelectors :many
-- The unannotated rule or block a limited one needs beside it, when the
-- principal already holds it.
SELECT grants.id, grants.expires_at
FROM principal_grants AS grants
WHERE grants.organization_id = @organization_id
  AND grants.principal_urn = @principal_urn
  AND grants.scope = @scope
  AND grants.selectors = @selectors::jsonb
FOR UPDATE;

-- name: InsertGrant :execrows
INSERT INTO principal_grants (organization_id, principal_urn, scope, selectors, expires_at)
VALUES (@organization_id, @principal_urn, @scope, @selectors::jsonb, sqlc.narg('expires_at'));

-- name: SetGrantExpiry :execrows
UPDATE principal_grants
SET expires_at = sqlc.narg('expires_at'), updated_at = clock_timestamp()
WHERE id = @id
  AND organization_id = @organization_id;
