-- Serialises encryption configuration writes for one organization. A disabled
-- organization is not locked, so nothing new is provisioned for it.
-- name: LockOrganizationForEncryption :one
SELECT id
FROM organization_metadata
WHERE id = @organization_id
  AND disabled_at IS NULL
FOR NO KEY UPDATE;

-- name: GetOrganizationEncryptionConfig :one
SELECT *
FROM organization_encryption_configs
WHERE organization_id = @organization_id
  AND deleted IS FALSE;

-- name: CreateOrganizationEncryptionConfig :one
INSERT INTO organization_encryption_configs (organization_id, mode, state, kek_external_key_id)
VALUES (@organization_id, @mode, @state, @kek_external_key_id)
RETURNING *;

-- A GCP KMS KEK. Managed KEKs record the platform credential; customer KEKs
-- record one of the organization's, so one unwrap path serves both.
-- name: CreateEncryptionExternalKey :one
INSERT INTO external_keys (organization_id, external_credential_id, provider, purpose, algorithm, name)
VALUES (@organization_id, @external_credential_id, 'gcp_kms', 'encrypt', @algorithm, @name)
RETURNING *;

-- The subtype row for a GCP KMS KEK. The supertype foreign key checks only id
-- and provider, so the insert selects the key with the organization predicate
-- and inserts nothing for another organization's key.
-- name: CreateEncryptionGcpKmsKey :one
INSERT INTO gcp_kms_keys (external_key_id, resource_name)
SELECT ek.id, @resource_name
FROM external_keys AS ek
WHERE ek.id = @external_key_id
  AND ek.organization_id = @organization_id::text
  AND ek.provider = 'gcp_kms'
RETURNING external_key_id;

-- name: CreateManagedEncryptionKey :exec
INSERT INTO managed_encryption_keys (external_key_id, organization_id)
VALUES (@external_key_id, @organization_id);

-- Everything needed to use a KEK: its purpose and provider, the GCP resource,
-- whether Speakeasy minted it, and the credential that reaches it. A customer
-- key must use one of the organization's credentials; only a managed key may
-- use a platform credential.
-- name: GetEncryptionKeyForUse :one
SELECT
  ek.id,
  ek.organization_id,
  ek.provider,
  ek.purpose,
  ek.deleted,
  gcp.resource_name,
  (mek.external_key_id IS NOT NULL)::boolean AS managed,
  mek.kms_disabled_at,
  ec.id AS credential_id,
  gic.impersonate_service_account,
  gic.wif_pool_id,
  gic.wif_provider_id,
  gic.wif_project_number,
  gic.skip_project_verification
FROM external_keys AS ek
LEFT JOIN gcp_kms_keys AS gcp ON gcp.external_key_id = ek.id
LEFT JOIN managed_encryption_keys AS mek
       ON mek.external_key_id = ek.id
      AND mek.organization_id = ek.organization_id
LEFT JOIN external_credentials AS ec
       ON ec.id = ek.external_credential_id
      AND (
        ec.organization_id = ek.organization_id
        OR (ec.organization_id IS NULL AND mek.external_key_id IS NOT NULL)
      )
      AND ec.project_id IS NULL
      AND ec.provider = 'gcp_iam'
      AND ec.deleted IS FALSE
LEFT JOIN gcp_iam_credentials AS gic
       ON gic.external_credential_id = ec.id
WHERE ek.id = @id
  AND ek.organization_id = @organization_id::text;

-- name: GetActiveDataEncryptionKey :one
SELECT *
FROM data_encryption_keys
WHERE organization_id = @organization_id
  AND use_case = @use_case
  AND state = 'active'
  AND deleted IS FALSE;

-- A DEK that may still decrypt: active or decrypt_only.
-- name: GetReadableDataEncryptionKey :one
SELECT *
FROM data_encryption_keys
WHERE id = @id
  AND organization_id = @organization_id
  AND state IN ('active', 'decrypt_only')
  AND deleted IS FALSE;

-- The id is chosen by the caller because the KMS wrap binds it as associated
-- data before the row exists.
-- name: CreateDataEncryptionKey :one
INSERT INTO data_encryption_keys (id, organization_id, encryption_config_id, use_case, state, activated_at)
VALUES (@id, @organization_id, @encryption_config_id, @use_case, 'active', clock_timestamp())
RETURNING *;

-- name: RetireActiveDataEncryptionKey :one
UPDATE data_encryption_keys
SET state = 'decrypt_only', retired_at = clock_timestamp(), updated_at = clock_timestamp()
WHERE organization_id = @organization_id
  AND use_case = @use_case
  AND state = 'active'
  AND deleted IS FALSE
RETURNING *;

-- DEKs whose wraps must follow a KEK switch.
-- name: ListReadableDataEncryptionKeys :many
SELECT *
FROM data_encryption_keys
WHERE organization_id = @organization_id
  AND state IN ('active', 'decrypt_only')
  AND deleted IS FALSE
ORDER BY id;

-- name: CreateDataEncryptionKeyWrap :one
INSERT INTO data_encryption_key_wraps (organization_id, data_encryption_key_id, external_key_id, external_key_version, wrapped_key)
VALUES (@organization_id, @data_encryption_key_id, @external_key_id, @external_key_version, @wrapped_key)
RETURNING *;

-- name: GetDataEncryptionKeyWrap :one
SELECT *
FROM data_encryption_key_wraps
WHERE organization_id = @organization_id
  AND data_encryption_key_id = @data_encryption_key_id
  AND external_key_id = @external_key_id
  AND deleted IS FALSE;

-- name: ReplaceDataEncryptionKeyWrap :one
UPDATE data_encryption_key_wraps
SET wrapped_key = @wrapped_key, external_key_version = @external_key_version, updated_at = clock_timestamp()
WHERE id = @id
  AND organization_id = @organization_id
  AND deleted IS FALSE
RETURNING *;

-- name: TouchDataEncryptionKeyWrapUnwrapped :exec
UPDATE data_encryption_key_wraps
SET last_unwrapped_at = clock_timestamp()
WHERE id = @id
  AND organization_id = @organization_id;

-- name: SoftDeleteDataEncryptionKeyWrapsForKey :many
UPDATE data_encryption_key_wraps
SET deleted_at = clock_timestamp(), updated_at = clock_timestamp()
WHERE organization_id = @organization_id
  AND external_key_id = @external_key_id
  AND deleted IS FALSE
RETURNING *;

-- Every readable DEK and its live wrap under one KEK (NULL when missing), for
-- the switch and the health probe.
-- name: ListDataEncryptionKeyWrapsForKey :many
SELECT
  sqlc.embed(dek),
  w.id AS wrap_id,
  w.wrapped_key,
  w.external_key_version,
  w.last_unwrapped_at
FROM data_encryption_keys AS dek
LEFT JOIN data_encryption_key_wraps AS w
       ON w.organization_id = dek.organization_id
      AND w.data_encryption_key_id = dek.id
      AND w.external_key_id = @external_key_id
      AND w.deleted IS FALSE
WHERE dek.organization_id = @organization_id
  AND dek.state IN ('active', 'decrypt_only')
  AND dek.deleted IS FALSE
ORDER BY dek.id;

-- Locks the KEK a switch moves to. FOR SHARE conflicts with the FOR UPDATE the
-- key delete takes, so a key deleted after the switch verified it is seen as
-- gone here rather than becoming the organization's KEK.
-- name: LockEncryptionKeyForSwitch :one
SELECT ek.id, (mek.external_key_id IS NOT NULL)::boolean AS managed, mek.kms_disabled_at
FROM external_keys AS ek
LEFT JOIN managed_encryption_keys AS mek
       ON mek.external_key_id = ek.id
      AND mek.organization_id = ek.organization_id
WHERE ek.id = @id
  AND ek.organization_id = @organization_id::text
  AND ek.purpose = 'encrypt'
  AND ek.deleted IS FALSE
FOR SHARE OF ek;

-- name: StartEncryptionConfigSwitch :one
UPDATE organization_encryption_configs
SET state = 'switching',
    previous_kek_external_key_id = kek_external_key_id,
    kek_external_key_id = @kek_external_key_id,
    mode = @mode,
    switch_started_at = clock_timestamp(),
    switch_completed_at = NULL,
    updated_at = clock_timestamp()
WHERE organization_id = @organization_id
  AND state = 'active'
  AND deleted IS FALSE
RETURNING *;

-- name: CompleteEncryptionConfigSwitch :one
UPDATE organization_encryption_configs
SET state = 'active',
    previous_kek_external_key_id = NULL,
    switch_completed_at = clock_timestamp(),
    updated_at = clock_timestamp()
WHERE organization_id = @organization_id
  AND state = 'switching'
  AND deleted IS FALSE
RETURNING *;

-- name: RollbackEncryptionConfigSwitch :one
UPDATE organization_encryption_configs
SET state = 'active',
    kek_external_key_id = previous_kek_external_key_id,
    previous_kek_external_key_id = NULL,
    mode = @mode,
    switch_started_at = NULL,
    updated_at = clock_timestamp()
WHERE organization_id = @organization_id
  AND state = 'switching'
  AND previous_kek_external_key_id IS NOT NULL
  AND deleted IS FALSE
RETURNING *;

-- name: SuspendEncryptionConfig :one
UPDATE organization_encryption_configs
SET state = 'suspended',
    suspended_at = clock_timestamp(),
    suspended_reason = @suspended_reason,
    updated_at = clock_timestamp()
WHERE organization_id = @organization_id
  AND state <> 'suspended'
  AND deleted IS FALSE
RETURNING *;

-- A suspended configuration resumes into 'switching' when a switch was in
-- flight, so the previous KEK's wraps stay readable.
-- name: ResumeEncryptionConfig :one
UPDATE organization_encryption_configs
SET state = CASE WHEN previous_kek_external_key_id IS NULL THEN 'active' ELSE 'switching' END,
    suspended_at = NULL,
    suspended_reason = NULL,
    updated_at = clock_timestamp()
WHERE organization_id = @organization_id
  AND state = 'suspended'
  AND deleted IS FALSE
RETURNING *;

-- name: RecordEncryptionKeyCheck :exec
UPDATE organization_encryption_configs
SET last_kek_check_at = clock_timestamp(),
    last_kek_check_error = sqlc.narg(last_kek_check_error),
    updated_at = clock_timestamp()
WHERE organization_id = @organization_id
  AND deleted IS FALSE;

-- Configurations the health probe should look at next, oldest check first.
-- Disabled organizations are skipped.
-- name: ListEncryptionConfigsDueForCheck :many
SELECT c.organization_id
FROM organization_encryption_configs AS c
JOIN organization_metadata AS o ON o.id = c.organization_id AND o.disabled_at IS NULL
WHERE c.deleted IS FALSE
  AND (c.last_kek_check_at IS NULL OR c.last_kek_check_at < clock_timestamp() - make_interval(secs => @check_interval_seconds::integer))
ORDER BY c.last_kek_check_at NULLS FIRST, c.organization_id
LIMIT @page_size;

-- Managed KEKs nothing depends on any more: no live configuration names them,
-- no live wrap uses them, and no wrap under them was dropped after
-- @retired_before. The retention keeps a key that just stopped protecting data
-- enabled for a while, so a completed switch can still be undone by restoring
-- the wraps. Their KMS key is disabled by the sweep.
-- name: ListManagedEncryptionKeysPendingDisable :many
SELECT mek.external_key_id, mek.organization_id, gcp.resource_name
FROM managed_encryption_keys AS mek
JOIN gcp_kms_keys AS gcp ON gcp.external_key_id = mek.external_key_id
WHERE mek.kms_disabled_at IS NULL
  AND NOT EXISTS (
    SELECT 1 FROM organization_encryption_configs AS c
    WHERE c.organization_id = mek.organization_id
      AND c.deleted IS FALSE
      AND (c.kek_external_key_id = mek.external_key_id OR c.previous_kek_external_key_id = mek.external_key_id)
  )
  AND NOT EXISTS (
    SELECT 1 FROM data_encryption_key_wraps AS w
    WHERE w.organization_id = mek.organization_id
      AND w.external_key_id = mek.external_key_id
      AND (w.deleted IS FALSE OR w.deleted_at > @retired_before::timestamptz)
  )
ORDER BY mek.external_key_id
LIMIT @page_size;

-- Re-checks, under the organization lock, that a managed KEK is still unused,
-- past its retention, and not yet disabled, so the sweep never disables a key
-- a concurrent switch just started using.
-- name: ManagedEncryptionKeyIsUnused :one
SELECT EXISTS (
  SELECT 1
  FROM managed_encryption_keys AS mek
  WHERE mek.external_key_id = @external_key_id
    AND mek.organization_id = @organization_id::text
    AND mek.kms_disabled_at IS NULL
    AND NOT EXISTS (
      SELECT 1 FROM organization_encryption_configs AS c
      WHERE c.organization_id = mek.organization_id
        AND c.deleted IS FALSE
        AND (c.kek_external_key_id = mek.external_key_id OR c.previous_kek_external_key_id = mek.external_key_id)
    )
    AND NOT EXISTS (
      SELECT 1 FROM data_encryption_key_wraps AS w
      WHERE w.organization_id = mek.organization_id
        AND w.external_key_id = mek.external_key_id
        AND (w.deleted IS FALSE OR w.deleted_at > @retired_before::timestamptz)
    )
) AS unused;

-- name: MarkManagedEncryptionKeyDisabled :exec
UPDATE managed_encryption_keys
SET kms_disabled_at = clock_timestamp(), updated_at = clock_timestamp()
WHERE external_key_id = @external_key_id
  AND organization_id = @organization_id
  AND kms_disabled_at IS NULL;

-- Whether an external key wraps any DEK or backs a live configuration, for the
-- external key delete guard.
-- name: ExternalKeyProtectsOrganizationData :one
SELECT (
  EXISTS (
    SELECT 1 FROM data_encryption_key_wraps AS w
    WHERE w.organization_id = @organization_id
      AND w.external_key_id = @external_key_id
      AND w.deleted IS FALSE
  )
  OR EXISTS (
    SELECT 1 FROM organization_encryption_configs AS c
    WHERE c.organization_id = @organization_id
      AND c.deleted IS FALSE
      AND (c.kek_external_key_id = @external_key_id OR c.previous_kek_external_key_id = @external_key_id)
  )
)::boolean AS protects;

-- The platform-tier credential managed KEKs are reached with. Only rows that
-- belong to no organization or project qualify.
-- name: GetPlatformGcpIamCredential :one
SELECT sqlc.embed(ec), sqlc.embed(gic)
FROM external_credentials AS ec
JOIN gcp_iam_credentials AS gic ON gic.external_credential_id = ec.id
WHERE ec.id = @id
  AND ec.organization_id IS NULL
  AND ec.project_id IS NULL
  AND ec.provider = 'gcp_iam'
  AND ec.deleted IS FALSE;
