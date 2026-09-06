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
  wif_project_number,
  skip_project_verification
) VALUES (
  @external_credential_id,
  sqlc.narg('impersonate_service_account'),
  sqlc.narg('wif_pool_id'),
  sqlc.narg('wif_provider_id'),
  sqlc.narg('wif_project_number'),
  @skip_project_verification
)
RETURNING *;

-- Tenancy for the reads and writes below is a single nullable parameter:
-- @organization_id is a customer organization for organization-scoped rows, or
-- NULL for platform-scoped rows (Gram's own credentials, managed by the
-- platform-admin surface). IS NOT DISTINCT FROM makes NULL match NULL, so both
-- tiers share one query set; the platform-admin handlers pass a NULL
-- organization_id, exactly as they already do for CreateExternalCredential.
-- project_id is always NULL for these rows in both tiers.

-- name: GetAwsIamCredential :one
SELECT sqlc.embed(ec), sqlc.embed(aws)
FROM external_credentials AS ec
JOIN aws_iam_credentials AS aws ON aws.external_credential_id = ec.id
WHERE ec.id = @id
  AND ec.organization_id IS NOT DISTINCT FROM @organization_id
  AND ec.project_id IS NULL
  AND ec.provider = 'aws_iam'
  AND ec.deleted IS FALSE;

-- name: GetGcpIamCredential :one
SELECT sqlc.embed(ec), sqlc.embed(gcp)
FROM external_credentials AS ec
JOIN gcp_iam_credentials AS gcp ON gcp.external_credential_id = ec.id
WHERE ec.id = @id
  AND ec.organization_id IS NOT DISTINCT FROM @organization_id
  AND ec.project_id IS NULL
  AND ec.provider = 'gcp_iam'
  AND ec.deleted IS FALSE;

-- name: ListExternalCredentials :many
SELECT *
FROM external_credentials
WHERE organization_id IS NOT DISTINCT FROM @organization_id
  AND project_id IS NULL
  AND deleted IS FALSE
  AND (sqlc.narg('provider')::text IS NULL OR provider = sqlc.narg('provider')::text)
ORDER BY id DESC;

-- name: UpdateExternalCredential :one
UPDATE external_credentials
SET name = @name,
    updated_at = clock_timestamp()
WHERE id = @id
  AND organization_id IS NOT DISTINCT FROM @organization_id
  AND project_id IS NULL
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
    skip_project_verification = @skip_project_verification,
    updated_at = clock_timestamp()
WHERE external_credential_id = @external_credential_id
RETURNING *;

-- Locks the external credential row so a preflight run against it cannot be
-- raced by a concurrent key write. FOR UPDATE is load-bearing and must not be
-- weakened to FOR NO KEY UPDATE or FOR SHARE: inserting or re-pointing an
-- external_keys row takes FOR KEY SHARE on this parent row through
-- external_keys_external_credential_id_fkey, which conflicts with FOR UPDATE but
-- not with the weaker modes. Downgrading the lock would silently reopen the
-- TOCTOU window where a key write commits between the preflight and the soft
-- delete, leaving a live key behind a deleted credential.
--
-- This is the only row lock the credential delete takes, and the key delete in
-- externalkeys locks only the key, so neither path holds one lock while waiting
-- on the other and the two cannot deadlock.
-- name: LockExternalCredentialForUpdate :one
SELECT id
FROM external_credentials
WHERE id = @id
  AND organization_id IS NOT DISTINCT FROM @organization_id
  AND project_id IS NULL
  AND provider = @provider
  AND deleted IS FALSE
FOR UPDATE;

-- Reports whether any live external key still names the credential, which
-- refuses the delete. Run inside the delete transaction, after taking the row
-- lock.
--
-- Soft-deleted keys do not count: a deleted key signs nothing, so it cannot be
-- broken by removing the credential that reached it.
--
-- The database will not enforce this. external_credentials.deleted is a
-- generated column, so the soft delete is an UPDATE and
-- external_keys_external_credential_id_fkey never fires — which is exactly why
-- soft-deleting a credential today silently orphans every key behind it.
--
-- The parameter is explicitly cast because external_keys carries its own
-- external_credential_id column, which leaves sqlc unable to infer the named
-- parameter's type from the column reference alone. The check is a single
-- EXISTS: sqlc types `EXISTS(...) OR EXISTS(...)` as pgtype.Bool, and an unset
-- value there would read as "not referenced" and fail this preflight open.
-- name: SoftDeleteExternalCredentialPreflight :one
SELECT EXISTS (
  SELECT 1
  FROM external_keys
  WHERE external_credential_id = @external_credential_id::uuid
    AND deleted IS FALSE
);

-- name: SoftDeleteExternalCredential :one
UPDATE external_credentials
SET deleted_at = clock_timestamp()
WHERE id = @id
  AND organization_id IS NOT DISTINCT FROM @organization_id
  AND project_id IS NULL
  AND provider = @provider
  AND deleted IS FALSE
RETURNING *;
