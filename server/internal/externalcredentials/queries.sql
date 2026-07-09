-- name: CreateExternalCredential :one
INSERT INTO external_credentials (organization_id, provider, name)
VALUES (@organization_id, @provider, @name)
RETURNING *;

-- name: CreateAwsIamCredential :one
INSERT INTO aws_iam_credentials (
  external_credential_id,
  assume_role_arn,
  external_id,
  oidc_audience,
  oidc_subject,
  sts_region
) VALUES (
  @external_credential_id,
  sqlc.narg('assume_role_arn'),
  sqlc.narg('external_id'),
  sqlc.narg('oidc_audience'),
  sqlc.narg('oidc_subject'),
  sqlc.narg('sts_region')
)
RETURNING *;

-- name: CreateGcpIamCredential :one
INSERT INTO gcp_iam_credentials (
  external_credential_id,
  impersonate_service_account,
  wif_pool_id,
  wif_provider_id,
  wif_project_number
) VALUES (
  @external_credential_id,
  sqlc.narg('impersonate_service_account'),
  sqlc.narg('wif_pool_id'),
  sqlc.narg('wif_provider_id'),
  sqlc.narg('wif_project_number')
)
RETURNING *;

-- name: GetAwsIamCredential :one
SELECT sqlc.embed(ec), sqlc.embed(aws)
FROM external_credentials AS ec
JOIN aws_iam_credentials AS aws ON aws.external_credential_id = ec.id
WHERE ec.id = @id
  AND ec.organization_id = @organization_id
  AND ec.provider = 'aws_iam'
  AND ec.deleted IS FALSE;

-- name: GetGcpIamCredential :one
SELECT sqlc.embed(ec), sqlc.embed(gcp)
FROM external_credentials AS ec
JOIN gcp_iam_credentials AS gcp ON gcp.external_credential_id = ec.id
WHERE ec.id = @id
  AND ec.organization_id = @organization_id
  AND ec.provider = 'gcp_iam'
  AND ec.deleted IS FALSE;

-- name: ListExternalCredentials :many
SELECT *
FROM external_credentials
WHERE organization_id = @organization_id
  AND deleted IS FALSE
  AND (sqlc.narg('provider')::text IS NULL OR provider = sqlc.narg('provider')::text)
ORDER BY id DESC;

-- name: UpdateExternalCredential :one
UPDATE external_credentials
SET name = @name,
    updated_at = clock_timestamp()
WHERE id = @id
  AND organization_id = @organization_id
  AND deleted IS FALSE
RETURNING *;

-- Subtype update: keyed on external_credential_id only. Callers must first
-- verify org + provider ownership via GetAwsIamCredential (scoped by
-- organization_id, provider, and deleted IS FALSE) in the same transaction.
-- name: UpdateAwsIamCredential :one
UPDATE aws_iam_credentials
SET assume_role_arn = sqlc.narg('assume_role_arn'),
    external_id = sqlc.narg('external_id'),
    oidc_audience = sqlc.narg('oidc_audience'),
    oidc_subject = sqlc.narg('oidc_subject'),
    sts_region = sqlc.narg('sts_region'),
    updated_at = clock_timestamp()
WHERE external_credential_id = @external_credential_id
RETURNING *;

-- Subtype update: keyed on external_credential_id only. Callers must first
-- verify org + provider ownership via GetGcpIamCredential (scoped by
-- organization_id, provider, and deleted IS FALSE) in the same transaction.
-- name: UpdateGcpIamCredential :one
UPDATE gcp_iam_credentials
SET impersonate_service_account = sqlc.narg('impersonate_service_account'),
    wif_pool_id = sqlc.narg('wif_pool_id'),
    wif_provider_id = sqlc.narg('wif_provider_id'),
    wif_project_number = sqlc.narg('wif_project_number'),
    updated_at = clock_timestamp()
WHERE external_credential_id = @external_credential_id
RETURNING *;

-- name: SoftDeleteExternalCredential :one
UPDATE external_credentials
SET deleted_at = clock_timestamp()
WHERE id = @id
  AND organization_id = @organization_id
  AND provider = @provider
  AND deleted IS FALSE
RETURNING *;
