-- name: GetKillswitchPrescriptionIdentity :one
SELECT
  id,
  organization_id,
  definition_key,
  principal_kind,
  principal_key,
  resource_kind,
  current_version
FROM killswitch_prescriptions
WHERE organization_id = @organization_id
  AND id = @prescription_id;

-- name: ClaimKillswitchOperation :one
WITH database_time AS (
  SELECT clock_timestamp() AS now
)
INSERT INTO killswitch_operations (
  organization_id,
  operation_id,
  actor_user_id,
  operation,
  request_hash,
  status,
  response,
  expires_at,
  created_at,
  updated_at
)
SELECT
  @organization_id,
  @operation_id,
  @actor_user_id,
  @operation,
  @request_hash,
  'pending',
  NULL,
  database_time.now + interval '30 days',
  database_time.now,
  database_time.now
FROM database_time
ON CONFLICT (organization_id, operation_id) DO UPDATE
SET actor_user_id = EXCLUDED.actor_user_id,
    operation = EXCLUDED.operation,
    request_hash = EXCLUDED.request_hash,
    status = 'pending',
    response = NULL,
    expires_at = EXCLUDED.expires_at,
    created_at = EXCLUDED.created_at,
    updated_at = EXCLUDED.updated_at
WHERE killswitch_operations.expires_at <= EXCLUDED.created_at
RETURNING operation_id;

-- name: LockKillswitchOperation :one
SELECT operation, request_hash, status, response
FROM killswitch_operations
WHERE organization_id = @organization_id
  AND operation_id = @operation_id
FOR UPDATE;

-- name: CompleteKillswitchOperation :execrows
UPDATE killswitch_operations
SET status = 'completed',
    response = @response,
    updated_at = clock_timestamp()
WHERE organization_id = @organization_id
  AND operation_id = @operation_id
  AND operation = @operation
  AND request_hash = @request_hash
  AND status = 'pending';

-- name: DeleteExpiredKillswitchOperations :execrows
WITH expired AS (
  SELECT operation_id
  FROM killswitch_operations
  WHERE organization_id = @organization_id
    AND expires_at <= clock_timestamp()
  ORDER BY expires_at, operation_id
  FOR UPDATE SKIP LOCKED
  LIMIT @batch_size
)
DELETE FROM killswitch_operations AS operation
USING expired
WHERE operation.organization_id = @organization_id
  AND operation.operation_id = expired.operation_id;

-- name: LockKillswitchPrescriptionCurrent :one
SELECT id, organization_id, definition_key, principal_kind, principal_key, resource_kind, current_version
FROM killswitch_prescriptions
WHERE organization_id = @organization_id
  AND id = @prescription_id
FOR UPDATE;

-- name: GetKillswitchPrescriptionVersion :one
SELECT state, resource_scope, starts_at, expires_at, activated_at, superseded_at, internal_note, external_note
FROM killswitch_prescription_versions
WHERE organization_id = @organization_id
  AND prescription_id = @prescription_id
  AND version = @version;

-- name: GetKillswitchDatabaseTime :one
SELECT clock_timestamp()::timestamptz;

-- name: CreateKillswitchPrescriptionHeader :one
INSERT INTO killswitch_prescriptions (
  organization_id,
  definition_key,
  principal_kind,
  principal_key,
  resource_kind,
  current_version
) VALUES (
  @organization_id,
  @definition_key,
  @principal_kind,
  @principal_key,
  @resource_kind,
  1
)
RETURNING id;

-- name: CreateKillswitchPrescriptionVersion :execrows
INSERT INTO killswitch_prescription_versions (
  organization_id,
  prescription_id,
  version,
  state,
  resource_scope,
  starts_at,
  expires_at,
  activated_at,
  superseded_at,
  internal_note,
  external_note
) VALUES (
  @organization_id,
  @prescription_id,
  @version,
  @state,
  @resource_scope,
  @starts_at,
  @expires_at,
  @activated_at,
  NULL,
  @internal_note,
  @external_note
);

-- name: CreateKillswitchPrescriptionVersionResources :execrows
INSERT INTO killswitch_prescription_version_resources (
  organization_id,
  prescription_id,
  version,
  resource_key
)
SELECT
  @organization_id,
  @prescription_id,
  @version,
  resource_key
FROM unnest(@resource_keys::text[]) AS resource_key;

-- name: CopyKillswitchPrescriptionVersionResources :execrows
INSERT INTO killswitch_prescription_version_resources (
  organization_id,
  prescription_id,
  version,
  resource_key
)
SELECT
  source.organization_id,
  source.prescription_id,
  @new_version,
  source.resource_key
FROM killswitch_prescription_version_resources AS source
WHERE source.organization_id = @organization_id
  AND source.prescription_id = @prescription_id
  AND source.version = @source_version
ORDER BY source.resource_key
LIMIT 1001;

-- name: SupersedeKillswitchPrescriptionVersion :execrows
UPDATE killswitch_prescription_versions
SET superseded_at = @superseded_at
WHERE organization_id = @organization_id
  AND prescription_id = @prescription_id
  AND version = @version
  AND superseded_at IS NULL;

-- name: AdvanceKillswitchPrescriptionCurrentVersion :execrows
UPDATE killswitch_prescriptions
SET current_version = @new_version,
    updated_at = @updated_at
WHERE organization_id = @organization_id
  AND id = @prescription_id
  AND current_version = @expected_version;

-- name: ListKillswitchPrescriptionVersionResources :many
SELECT resource_key
FROM killswitch_prescription_version_resources
WHERE organization_id = @organization_id
  AND prescription_id = @prescription_id
  AND version = @version
ORDER BY resource_key
LIMIT 1001;
