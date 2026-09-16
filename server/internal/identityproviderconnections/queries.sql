-- Every query here is organization-qualified. Rows the provisioner writes go
-- through the owning repos (externalkeys, jsonwebkeysets, remotesessions).

-- name: CreateIdentityProviderConnection :one
INSERT INTO identity_provider_connections (organization_id, provider)
VALUES (@organization_id, @provider)
RETURNING *;

-- name: GetIdentityProviderConnection :one
SELECT *
FROM identity_provider_connections
WHERE id = @id
  AND organization_id = @organization_id
  AND deleted IS FALSE;

-- The revoke path reads through the tombstone so a deleted connection can
-- still withdraw its keys.
-- name: GetIdentityProviderConnectionIncludingDeleted :one
SELECT *
FROM identity_provider_connections
WHERE id = @id
  AND organization_id = @organization_id;

-- name: SoftDeleteIdentityProviderConnection :one
UPDATE identity_provider_connections
SET deleted_at = clock_timestamp(),
    updated_at = clock_timestamp()
WHERE id = @id
  AND organization_id = @organization_id
  AND deleted IS FALSE
RETURNING *;

-- Serializes provisioning and rotation per connection.
-- name: LockIdentityProviderConnectionForProvisioning :one
SELECT *
FROM identity_provider_connections
WHERE id = @id
  AND organization_id = @organization_id
  AND deleted IS FALSE
FOR UPDATE;

-- Organization-level only: a project-owned or platform issuer never carries a
-- managed client.
-- name: GetOrganizationRemoteSessionIssuerForProvisioning :one
SELECT *
FROM remote_session_issuers
WHERE id = @id
  AND organization_id = @organization_id
  AND project_id IS NULL
  AND deleted IS FALSE;

-- Speakeasy's own signing credential: platform-tier (organization_id IS NULL).
-- name: GetPlatformGcpIamCredentialForProvisioning :one
SELECT sqlc.embed(ec), sqlc.embed(gic)
FROM external_credentials AS ec
JOIN gcp_iam_credentials AS gic ON gic.external_credential_id = ec.id
WHERE ec.id = @id
  AND ec.organization_id IS NULL
  AND ec.project_id IS NULL
  AND ec.provider = 'gcp_iam'
  AND ec.deleted IS FALSE;

-- The managed client, its set, and the set's active key. The key join is LEFT
-- so a revoked connection (live client, live set, no live keys) still resolves.
-- name: GetManagedClient :one
SELECT
  sqlc.embed(c),
  s.id AS json_web_key_set_id,
  s.external_key_id,
  k.id AS json_web_key_id,
  k.kid,
  k.activated_at
FROM remote_session_clients AS c
JOIN json_web_key_sets AS s
  ON s.organization_id = c.organization_id
 AND s.id = c.json_web_key_set_id
 AND s.deleted IS FALSE
LEFT JOIN json_web_keys AS k
  ON k.organization_id = s.organization_id
 AND k.json_web_key_set_id = s.id
 AND k.state = 'active'
 AND k.deleted IS FALSE
WHERE c.organization_id = @organization_id
  AND c.identity_provider_connection_id = @identity_provider_connection_id
  AND c.deleted IS FALSE;
