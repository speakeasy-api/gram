-- name: IdentityProviderConnectionIsDeleted :one
-- Whether the connection behind a managed-by marker is tombstoned; a tombstone
-- turns the row into a leftover the organization may delete.
SELECT deleted
FROM identity_provider_connections
WHERE id = @id
  AND organization_id = @organization_id;
