-- Loads the backing credential's provider, scoped to the organization, so the
-- caller can validate that a key references a same-org, org-scoped credential
-- (project_id IS NULL, matching the org-scoping the key itself requires) of the
-- matching cloud family before writing. Run this inside the key write
-- transaction: external_credentials.deleted is a generated column, so a soft
-- delete never fires the external_keys foreign key, and validating without the
-- row lock would leave a TOCTOU window where a concurrent soft delete commits
-- between this read and the key write, producing a live key against a deleted
-- credential. FOR SHARE holds the credential row until the key write commits.
-- name: GetExternalCredentialProviderForKey :one
SELECT provider
FROM external_credentials
WHERE id = @external_credential_id
  AND organization_id = @organization_id
  AND deleted IS FALSE
  AND project_id IS NULL
FOR SHARE;

-- name: CreateExternalKey :one
INSERT INTO external_keys (
  organization_id,
  external_credential_id,
  provider,
  algorithm,
  name,
  customer_grant_reference
) VALUES (
  @organization_id,
  @external_credential_id,
  @provider,
  @algorithm,
  @name,
  sqlc.narg('customer_grant_reference')
)
RETURNING *;

-- name: CreateAwsKmsKey :one
INSERT INTO aws_kms_keys (external_key_id, key_arn)
VALUES (@external_key_id, @key_arn)
RETURNING *;

-- name: CreateGcpKmsKey :one
INSERT INTO gcp_kms_keys (external_key_id, resource_name)
VALUES (@external_key_id, @resource_name)
RETURNING *;

-- name: GetAwsKmsKey :one
SELECT sqlc.embed(ek), sqlc.embed(aws)
FROM external_keys AS ek
JOIN aws_kms_keys AS aws ON aws.external_key_id = ek.id
WHERE ek.id = @id
  AND ek.organization_id = @organization_id
  AND ek.provider = 'aws_kms'
  AND ek.deleted IS FALSE;

-- name: GetGcpKmsKey :one
SELECT sqlc.embed(ek), sqlc.embed(gcp)
FROM external_keys AS ek
JOIN gcp_kms_keys AS gcp ON gcp.external_key_id = ek.id
WHERE ek.id = @id
  AND ek.organization_id = @organization_id
  AND ek.provider = 'gcp_kms'
  AND ek.deleted IS FALSE;

-- name: ListExternalKeys :many
SELECT *
FROM external_keys
WHERE organization_id = @organization_id
  AND deleted IS FALSE
  AND (sqlc.narg('provider')::text IS NULL OR provider = sqlc.narg('provider')::text)
ORDER BY id DESC;

-- name: UpdateExternalKey :one
UPDATE external_keys
SET external_credential_id = @external_credential_id,
    algorithm = @algorithm,
    name = @name,
    customer_grant_reference = sqlc.narg('customer_grant_reference'),
    updated_at = clock_timestamp()
WHERE id = @id
  AND organization_id = @organization_id
  AND deleted IS FALSE
RETURNING *;

-- Subtype update: keyed on external_key_id only. Callers must first verify org +
-- provider ownership via GetAwsKmsKey (scoped by organization_id, provider, and
-- deleted IS FALSE) in the same transaction.
-- name: UpdateAwsKmsKey :one
UPDATE aws_kms_keys
SET key_arn = @key_arn,
    updated_at = clock_timestamp()
WHERE external_key_id = @external_key_id
RETURNING *;

-- Subtype update: keyed on external_key_id only. Callers must first verify org +
-- provider ownership via GetGcpKmsKey (scoped by organization_id, provider, and
-- deleted IS FALSE) in the same transaction.
-- name: UpdateGcpKmsKey :one
UPDATE gcp_kms_keys
SET resource_name = @resource_name,
    updated_at = clock_timestamp()
WHERE external_key_id = @external_key_id
RETURNING *;

-- name: SoftDeleteExternalKey :one
UPDATE external_keys
SET deleted_at = clock_timestamp()
WHERE id = @id
  AND organization_id = @organization_id
  AND provider = @provider
  AND deleted IS FALSE
RETURNING *;
