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

-- Loads everything the verify probe needs in one read: the key, its provider
-- subtype, and the GCP identity of the credential that reaches it.
--
-- The credential joins are LEFT so a key whose credential was soft-deleted still
-- returns the key. An inner join would surface that as no rows, which the caller
-- can only report as "key not found" — a lie about a key that plainly exists,
-- and a reachable one: external_credentials.deleted is a generated column, so a
-- soft delete never fires the external_keys foreign key. credential_id is NULL
-- in exactly that case, which is what lets the caller say so.
--
-- The join predicate spells out every condition the credential must meet rather
-- than matching on id alone, so a row that fails one of them reads as an absent
-- credential instead of joining and reporting some later, more confusing
-- symptom. validateBackingCredential already enforces all of them on both write
-- paths, so only a direct database edit reaches those cases.
--
-- No row lock is taken. Verify performs no write, so there is nothing for a
-- concurrent credential change to race: the probe reports the configuration as
-- it stood when it was read, which is all it ever claims to do.
-- name: GetGcpKmsKeyForVerify :one
SELECT
  sqlc.embed(ek),
  sqlc.embed(gcp),
  ec.id AS credential_id,
  gic.impersonate_service_account,
  gic.wif_pool_id,
  gic.wif_provider_id,
  gic.wif_project_number
FROM external_keys AS ek
JOIN gcp_kms_keys AS gcp ON gcp.external_key_id = ek.id
LEFT JOIN external_credentials AS ec
       ON ec.id = ek.external_credential_id
      AND ec.organization_id = ek.organization_id
      AND ec.project_id IS NULL
      AND ec.provider = 'gcp_iam'
      AND ec.deleted IS FALSE
LEFT JOIN gcp_iam_credentials AS gic
       ON gic.external_credential_id = ec.id
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

-- Updates only the mutable columns. algorithm is absent on purpose, alongside
-- the subtype identity columns (aws_kms_keys.key_arn, gcp_kms_keys.resource_name)
-- which have no update query at all: an external_keys row must identify exactly
-- one signable key permanently, because json_web_keys pins each published kid to
-- the row it was minted from. There is deliberately no subtype update query to
-- pair with this one, so aws_kms_keys / gcp_kms_keys rows are write-once and
-- their updated_at always equals created_at.
-- name: UpdateExternalKey :one
UPDATE external_keys
SET external_credential_id = @external_credential_id,
    name = @name,
    customer_grant_reference = sqlc.narg('customer_grant_reference'),
    updated_at = clock_timestamp()
WHERE id = @id
  AND organization_id = @organization_id
  AND deleted IS FALSE
RETURNING *;

-- Locks the external key row so the JWKS reference check below cannot be raced
-- by a concurrent JWKS write. FOR UPDATE is load-bearing and must not be
-- weakened to FOR NO KEY UPDATE: inserting a json_web_key_sets / json_web_keys
-- row takes FOR KEY SHARE on this parent row, which conflicts with FOR UPDATE
-- but NOT with FOR NO KEY UPDATE. Downgrading the lock mode would silently
-- reopen the TOCTOU window where a JWKS insert commits between the reference
-- check and the soft delete. The JWKS create path takes the counterpart
-- FOR SHARE on this row (AIS-240).
-- name: LockExternalKeyForDelete :one
SELECT id
FROM external_keys
WHERE id = @id
  AND organization_id = @organization_id
  AND provider = @provider
  AND deleted IS FALSE
FOR UPDATE;

-- Reports whether any live JSON Web Key Set or published JSON Web Key still
-- references the external key. Run inside the delete transaction, after
-- LockExternalKeyForDelete.
--
-- Soft-deleted references do not count. Both JWKS tables use the same
-- deleted_at / generated deleted pattern as external_keys, a revoked key is
-- soft-deleted, and soft-deleting a set cascade-soft-deletes its keys (AIS-240),
-- so `deleted IS FALSE` is the live-reference test on both sides. The sets arm
-- is not redundant with the keys arm: a set that has published no keys yet, or
-- whose keys were all revoked, still references the external key.
--
-- The database will not enforce this. Both foreign keys omit ON DELETE, which is
-- NO ACTION (end-of-statement) rather than RESTRICT (immediate) — a distinction
-- that is load-bearing, because organization deletion cascades into external_keys
-- and json_web_key_sets in one statement and NO ACTION survives that ordering
-- while RESTRICT would abort it. Neither variant fires on the soft-delete path
-- anyway, since `deleted` is a generated column.
-- The parameters are explicitly cast because both tables carry an
-- organization_id / external_key_id column, which leaves sqlc unable to infer a
-- named parameter's type from the column reference alone. The two arms are one
-- EXISTS over a UNION ALL rather than two OR'd EXISTS so the result is a
-- non-nullable bool: sqlc types `EXISTS(...) OR EXISTS(...)` as pgtype.Bool,
-- which would make an unset value read as "not referenced" and fail this guard
-- open. UNION ALL is still short-circuiting here, since EXISTS stops at the
-- first row.
-- name: ExternalKeyHasJsonWebKeyReferences :one
SELECT EXISTS (
  SELECT 1
  FROM json_web_key_sets
  WHERE organization_id = @organization_id::text
    AND external_key_id = @external_key_id::uuid
    AND deleted IS FALSE
  UNION ALL
  SELECT 1
  FROM json_web_keys
  WHERE organization_id = @organization_id::text
    AND external_key_id = @external_key_id::uuid
    AND deleted IS FALSE
) AS referenced;

-- name: SoftDeleteExternalKey :one
UPDATE external_keys
SET deleted_at = clock_timestamp()
WHERE id = @id
  AND organization_id = @organization_id
  AND provider = @provider
  AND deleted IS FALSE
RETURNING *;
