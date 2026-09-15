-- name: CreateIdentityProviderConnection :one
INSERT INTO identity_provider_connections (
  organization_id,
  kind,
  tenant_identifier,
  status,
  capabilities
) VALUES (
  @organization_id,
  @kind,
  @tenant_identifier,
  'pending',
  '{}'
)
RETURNING *;

-- name: CreateIdentityProviderSigningKey :one
INSERT INTO identity_provider_signing_keys (
  organization_id,
  identity_provider_connection_id,
  kid,
  algorithm,
  public_jwk,
  private_key_encrypted,
  state,
  activated_at
) VALUES (
  @organization_id,
  @identity_provider_connection_id,
  @kid,
  @algorithm,
  @public_jwk,
  @private_key_encrypted,
  'active',
  clock_timestamp()
)
RETURNING *;

-- name: CreateOktaIdentityProviderConnection :one
INSERT INTO okta_identity_provider_connections (
  identity_provider_connection_id,
  okta_domain,
  auth_method,
  signing_key_id,
  granted_scopes
) VALUES (
  @identity_provider_connection_id,
  @okta_domain,
  'private_key_jwt',
  @signing_key_id,
  '{}'
)
RETURNING *;

-- name: GetIdentityProviderConnectionByOrganization :one
SELECT
  c.*,
  o.client_id,
  o.granted_scopes,
  o.signing_key_id,
  k.kid AS signing_key_kid
FROM identity_provider_connections AS c
JOIN okta_identity_provider_connections AS o
  ON o.identity_provider_connection_id = c.id
JOIN identity_provider_signing_keys AS k
  ON k.id = o.signing_key_id
  AND k.organization_id = c.organization_id
  AND k.identity_provider_connection_id = c.id
  AND k.deleted IS FALSE
WHERE c.organization_id = @organization_id
  AND c.deleted IS FALSE;

-- name: UpdateOktaIdentityProviderClientID :exec
UPDATE okta_identity_provider_connections AS o
SET
  client_id = @client_id,
  updated_at = clock_timestamp()
FROM identity_provider_connections AS c
WHERE o.identity_provider_connection_id = c.id
  AND c.organization_id = @organization_id
  AND c.id = @identity_provider_connection_id
  AND c.deleted IS FALSE;

-- name: MarkIdentityProviderAwaitingVerification :exec
UPDATE identity_provider_connections
SET
  status = 'awaiting_verification',
  status_detail = NULL,
  updated_at = clock_timestamp()
WHERE organization_id = @organization_id
  AND id = @identity_provider_connection_id
  AND deleted IS FALSE;

-- name: SoftDeleteIdentityProviderSigningKeys :exec
UPDATE identity_provider_signing_keys
SET
  deleted_at = clock_timestamp(),
  updated_at = clock_timestamp()
WHERE organization_id = @organization_id
  AND identity_provider_connection_id = @identity_provider_connection_id
  AND deleted IS FALSE;

-- name: SoftDeleteIdentityProviderConnection :exec
UPDATE identity_provider_connections
SET
  deleted_at = clock_timestamp(),
  updated_at = clock_timestamp()
WHERE organization_id = @organization_id
  AND id = @id
  AND deleted IS FALSE;

-- name: GetIdentityProviderSigningKey :one
SELECT *
FROM identity_provider_signing_keys
WHERE organization_id = @organization_id
  AND identity_provider_connection_id = @identity_provider_connection_id
  AND deleted IS FALSE
ORDER BY created_at DESC
LIMIT 1;

-- name: GetIdentityProviderJSONWebKeySet :one
SELECT jsonb_build_object(
  'keys',
  COALESCE(
    jsonb_agg(k.public_jwk ORDER BY k.created_at) FILTER (WHERE k.id IS NOT NULL),
    '[]'::jsonb
  )
)
FROM identity_provider_connections AS c
LEFT JOIN identity_provider_signing_keys AS k
  ON k.identity_provider_connection_id = c.id
  AND k.organization_id = c.organization_id
  AND k.deleted IS FALSE
WHERE c.id = @identity_provider_connection_id
  AND c.deleted IS FALSE
GROUP BY c.id;
