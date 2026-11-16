-- name: ListLimitedPrincipals :many
-- Principals holding a view or manage rule, or a block on either, that still
-- names a tool or a disposition. Limits are saved on connect rules only now.
SELECT grants.organization_id, grants.principal_urn
FROM principal_grants AS grants
WHERE grants.scope = ANY(@scopes::text[])
  AND (grants.selectors ? 'tool' OR grants.selectors ? 'disposition')
  AND (sqlc.narg('organization_id')::text IS NULL OR grants.organization_id = sqlc.narg('organization_id')::text)
GROUP BY grants.organization_id, grants.principal_urn
ORDER BY grants.organization_id, grants.principal_urn;

-- name: LockLimitedGrants :many
-- The limited rules of one principal, locked until the transaction ends.
SELECT grants.id, grants.scope, grants.selectors, grants.expires_at
FROM principal_grants AS grants
WHERE grants.organization_id = @organization_id
  AND grants.principal_urn = @principal_urn
  AND grants.scope = ANY(@scopes::text[])
  AND (grants.selectors ? 'tool' OR grants.selectors ? 'disposition')
ORDER BY grants.id
FOR UPDATE;

-- name: LockGrantBySelectors :many
-- The rule a limited one becomes once its limit is removed, when the principal
-- already holds it.
SELECT grants.id, grants.expires_at
FROM principal_grants AS grants
WHERE grants.organization_id = @organization_id
  AND grants.principal_urn = @principal_urn
  AND grants.scope = @scope
  AND grants.selectors = @selectors::jsonb
FOR UPDATE;

-- name: SetGrantSelectors :execrows
UPDATE principal_grants
SET selectors = @selectors::jsonb, updated_at = clock_timestamp()
WHERE id = @id
  AND organization_id = @organization_id;

-- name: SetGrantExpiry :execrows
UPDATE principal_grants
SET expires_at = sqlc.narg('expires_at'), updated_at = clock_timestamp()
WHERE id = @id
  AND organization_id = @organization_id;

-- name: DeleteGrant :execrows
DELETE FROM principal_grants
WHERE id = @id
  AND organization_id = @organization_id;
