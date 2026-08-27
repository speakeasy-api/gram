-- name: EvaluateCurrentPrescriptions :one
WITH evaluation_clock AS MATERIALIZED (
  SELECT clock_timestamp() AS database_now
),
definition_candidates AS (
  SELECT definition_key, definition_rank
  FROM unnest(@definition_keys::text[]) WITH ORDINALITY AS candidate(definition_key, definition_rank)
),
principal_candidates AS (
  SELECT principal_kind, principal_key, principal_rank
  FROM ROWS FROM (
    unnest(@principal_kinds::text[]),
    unnest(@principal_keys::text[])
  ) WITH ORDINALITY AS candidate(principal_kind, principal_key, principal_rank)
)
SELECT
  matched.prescription_id,
  matched.definition_key,
  matched.external_note
FROM definition_candidates AS definition_candidate
CROSS JOIN principal_candidates AS principal_candidate
CROSS JOIN LATERAL (
  SELECT
    prescription.id AS prescription_id,
    prescription.definition_key,
    version.external_note,
    CASE version.resource_scope WHEN 'selected' THEN 0 ELSE 1 END AS resource_scope_rank,
    version.starts_at,
    version.activated_at
  FROM killswitch_prescriptions AS prescription
  JOIN killswitch_prescription_versions AS version
    ON version.organization_id = prescription.organization_id
    AND version.prescription_id = prescription.id
    AND version.version = prescription.current_version
  CROSS JOIN evaluation_clock
  WHERE prescription.organization_id = @organization_id
    AND prescription.definition_key = definition_candidate.definition_key
    AND prescription.principal_kind = principal_candidate.principal_kind
    AND prescription.principal_key = principal_candidate.principal_key
    AND prescription.resource_kind = @resource_kind
    AND version.state = 'active'
    AND version.starts_at <= evaluation_clock.database_now
    AND (version.expires_at IS NULL OR version.expires_at > evaluation_clock.database_now)
    AND (
      version.resource_scope = 'all'
      OR (
        version.resource_scope = 'selected'
        AND EXISTS (
          SELECT 1
          FROM killswitch_prescription_version_resources AS resource
          WHERE resource.organization_id = @organization_id
            AND resource.organization_id = version.organization_id
            AND resource.prescription_id = version.prescription_id
            AND resource.version = version.version
            AND resource.resource_key = @resource_key
        )
      )
    )
  ORDER BY
    resource_scope_rank,
    version.starts_at DESC,
    version.activated_at DESC NULLS LAST,
    prescription.id ASC
  LIMIT 1
) AS matched
ORDER BY
  definition_candidate.definition_rank,
  matched.resource_scope_rank,
  principal_candidate.principal_rank,
  matched.starts_at DESC,
  matched.activated_at DESC NULLS LAST,
  matched.prescription_id ASC
LIMIT 1;

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
