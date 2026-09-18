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
-- Organization-level only: the client, its issuer, and the set all carry the
-- connection's organization scope, and the set must be marked by the same connection.
-- name: GetManagedClient :one
SELECT
  sqlc.embed(c),
  s.id AS json_web_key_set_id,
  s.external_key_id,
  k.id AS json_web_key_id,
  k.kid,
  k.activated_at
FROM remote_session_clients AS c
JOIN remote_session_issuers AS i
  ON i.id = c.remote_session_issuer_id
 AND i.organization_id = c.organization_id
 AND i.project_id IS NULL
 AND i.deleted IS FALSE
JOIN json_web_key_sets AS s
  ON s.organization_id = c.organization_id
 AND s.id = c.json_web_key_set_id
 AND s.identity_provider_connection_id = c.identity_provider_connection_id
 AND s.deleted IS FALSE
LEFT JOIN json_web_keys AS k
  ON k.organization_id = s.organization_id
 AND k.json_web_key_set_id = s.id
 AND k.state = 'active'
 AND k.deleted IS FALSE
WHERE c.organization_id = @organization_id
  AND c.project_id IS NULL
  AND c.identity_provider_connection_id = @identity_provider_connection_id
  AND c.deleted IS FALSE;

-- While pending, updated_at records observed publication (there is no published_at column).
-- A pending key is not timed until its publication commit has been observed.
-- These managed rows are organization-level (never project-owned).
-- name: MarkRotationPublicationUnobserved :exec
UPDATE json_web_keys SET updated_at = 'infinity'
WHERE id = @id AND organization_id = @organization_id AND state = 'pending' AND deleted IS FALSE;

-- Idempotent even when the publishing caller races a retry or revocation.
-- name: ObserveRotationPublication :one
UPDATE json_web_keys
SET updated_at = CASE WHEN updated_at = 'infinity' THEN clock_timestamp() ELSE updated_at END
WHERE id = @id AND organization_id = @organization_id AND state = 'pending' AND deleted IS FALSE
RETURNING updated_at;

-- Use the publication clock, not an application host's potentially skewed clock.
-- name: RotationPublicationReady :one
SELECT updated_at <= clock_timestamp() - make_interval(secs => @cache_seconds::integer)
FROM json_web_keys
WHERE id = @id AND organization_id = @organization_id AND state = 'pending' AND deleted IS FALSE;

-- Include tombstones and every organization: resource names are random and
-- global, so a reference anywhere means the key must never be disabled.
-- name: ManagedKeyResourceExists :one
SELECT EXISTS (
 SELECT 1 FROM gcp_kms_keys AS k
 WHERE k.resource_name = @resource_name
);

-- Test fixture: age a pending publication relative to the database clock.
-- name: BackdatePendingRotationPublication :exec
UPDATE json_web_keys
SET updated_at = clock_timestamp() - make_interval(secs => @age_seconds::integer)
WHERE json_web_key_set_id = @json_web_key_set_id
  AND organization_id = @organization_id AND state = 'pending' AND deleted IS FALSE;
