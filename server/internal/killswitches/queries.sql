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
),
compatible_definition_principals AS (
  SELECT definition_key, principal_kind
  FROM ROWS FROM (
    unnest(@compatible_definition_keys::text[]),
    unnest(@compatible_principal_kinds::text[])
  ) AS candidate(definition_key, principal_kind)
)
SELECT
  matched.prescription_id,
  matched.definition_key,
  matched.external_note
FROM definition_candidates AS definition_candidate
JOIN principal_candidates AS principal_candidate
  ON EXISTS (
    SELECT 1
    FROM compatible_definition_principals AS compatible
    WHERE compatible.definition_key = definition_candidate.definition_key
      AND compatible.principal_kind = principal_candidate.principal_kind
  )
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
SELECT actor_user_id, operation, request_hash, status, response
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

-- name: ListDueKillswitchExpiries :many
-- Privileged cross-organization discovery for the maintenance sweep. Killswitch
-- maintenance deliberately spans tenants; every subsequent mutation is
-- requalified by the candidate's organization_id under a row lock. The STABLE
-- statement_timestamp() keeps the expires_at comparison plannable as an index
-- bound; per-candidate eligibility is re-decided under the row lock.
SELECT organization_id, prescription_id, version
FROM killswitch_prescription_versions
WHERE state = 'active'
  AND expires_at IS NOT NULL
  AND expires_at <= statement_timestamp()
  AND (superseded_at IS NULL OR expires_at < superseded_at)
  AND NOT EXISTS (
    SELECT 1
    FROM killswitch_expiry_events AS marker
    WHERE marker.prescription_id = killswitch_prescription_versions.prescription_id
      AND marker.version = killswitch_prescription_versions.version
  )
ORDER BY expires_at, prescription_id, version
LIMIT @batch_size;

-- name: LockKillswitchVersionForExpiry :one
SELECT state, expires_at, superseded_at, clock_timestamp()::timestamptz AS database_now
FROM killswitch_prescription_versions
WHERE organization_id = @organization_id
  AND prescription_id = @prescription_id
  AND version = @version
FOR UPDATE;

-- name: RecordKillswitchExpiryEvent :execrows
INSERT INTO killswitch_expiry_events (
  organization_id,
  prescription_id,
  version
) VALUES (
  @organization_id,
  @prescription_id,
  @version
)
ON CONFLICT (prescription_id, version) DO NOTHING;

-- name: DeleteExpiredKillswitchOperationsGlobal :execrows
-- Privileged cross-organization retention cleanup for the maintenance sweep.
-- Receipts are deleted strictly by their database-anchored expires_at; replay
-- validity is decided by the claim query, never by cleanup timing. The STABLE
-- statement_timestamp() keeps the expires_at comparison plannable as an index
-- bound.
WITH expired AS (
  SELECT organization_id, operation_id
  FROM killswitch_operations
  WHERE expires_at <= statement_timestamp()
  ORDER BY expires_at, organization_id, operation_id
  FOR UPDATE SKIP LOCKED
  LIMIT @batch_size
)
DELETE FROM killswitch_operations AS operation
USING expired
WHERE operation.organization_id = expired.organization_id
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

-- name: GetKillswitchCurrentPrescription :one
SELECT
  prescription.id,
  prescription.organization_id,
  prescription.definition_key,
  prescription.principal_kind,
  prescription.principal_key,
  prescription.resource_kind,
  prescription.current_version,
  version.state,
  version.resource_scope,
  version.starts_at,
  version.expires_at,
  version.activated_at,
  version.superseded_at,
  version.internal_note,
  version.external_note,
  ARRAY(
    SELECT resource.resource_key
    FROM killswitch_prescription_version_resources AS resource
    WHERE resource.organization_id = prescription.organization_id
      AND resource.prescription_id = prescription.id
      AND resource.version = prescription.current_version
    ORDER BY resource.resource_key
    LIMIT 1001
  )::text[] AS selected_resource_keys
FROM killswitch_prescriptions AS prescription
JOIN killswitch_prescription_versions AS version
  ON version.organization_id = prescription.organization_id
  AND version.prescription_id = prescription.id
  AND version.version = prescription.current_version
WHERE prescription.organization_id = @organization_id
  AND prescription.id = @prescription_id;

-- name: ListKillswitchCurrentPrescriptions :many
SELECT
  prescription.id,
  prescription.organization_id,
  prescription.definition_key,
  prescription.principal_kind,
  prescription.principal_key,
  prescription.resource_kind,
  prescription.current_version,
  version.state,
  version.resource_scope,
  version.starts_at,
  version.expires_at,
  version.activated_at,
  version.superseded_at,
  version.internal_note,
  version.external_note,
  ARRAY(
    SELECT resource.resource_key
    FROM killswitch_prescription_version_resources AS resource
    WHERE resource.organization_id = prescription.organization_id
      AND resource.prescription_id = prescription.id
      AND resource.version = prescription.current_version
    ORDER BY resource.resource_key
    LIMIT 1001
  )::text[] AS selected_resource_keys
FROM killswitch_prescriptions AS prescription
JOIN killswitch_prescription_versions AS version
  ON version.organization_id = prescription.organization_id
  AND version.prescription_id = prescription.id
  AND version.version = prescription.current_version
WHERE prescription.organization_id = @organization_id
  AND (sqlc.narg('after_id')::uuid IS NULL OR prescription.id > sqlc.narg('after_id')::uuid)
ORDER BY prescription.id
LIMIT @result_limit;

-- name: ListCustomerKillswitches :many
WITH db_time AS (
  SELECT @status_as_of::timestamptz AS now
), current_rows AS (
  SELECT
    p.id, p.updated_at, p.principal_key AS user_id, v.version,
    v.resource_scope, v.starts_at, v.expires_at,
    CASE
      WHEN v.state = 'inactive' THEN 'lifted'
      WHEN v.starts_at > db_time.now THEN 'scheduled'
      WHEN v.expires_at IS NOT NULL AND v.expires_at <= db_time.now THEN 'expired'
      ELSE 'active'
    END::text AS customer_status,
    CASE WHEN v.starts_at > v.activated_at THEN 'scheduled' ELSE 'now' END::text AS customer_start,
    ARRAY(
      SELECT r.resource_key
      FROM killswitch_prescription_version_resources AS r
      WHERE r.organization_id = p.organization_id
        AND r.prescription_id = p.id
        AND r.version = v.version
      ORDER BY r.resource_key
      LIMIT 1001
    )::text[] AS selected_resource_keys
  FROM killswitch_prescriptions AS p
  JOIN killswitch_prescription_versions AS v
    ON v.organization_id = p.organization_id
   AND v.prescription_id = p.id
   AND v.version = p.current_version
  CROSS JOIN db_time
  WHERE p.organization_id = @organization_id
    AND p.definition_key = @definition_key
    AND p.principal_kind = @principal_kind
    AND p.resource_kind = @resource_kind
    AND (sqlc.narg('user_id')::text IS NULL OR p.principal_key = sqlc.narg('user_id')::text)
    AND (
      sqlc.narg('cursor_updated_at')::timestamptz IS NULL
      OR (p.updated_at, p.id) < (sqlc.narg('cursor_updated_at')::timestamptz, sqlc.narg('cursor_id')::uuid)
    )
)
SELECT *
FROM current_rows
WHERE sqlc.narg('customer_status')::text IS NULL
   OR customer_status = sqlc.narg('customer_status')::text
ORDER BY updated_at DESC, id DESC
LIMIT @result_limit;

-- name: ListCustomerKillswitchHistory :many
SELECT
  a.seq, a.action, a.actor_id, a.actor_type, a.actor_display_name, a.created_at,
  v.version, v.state, v.resource_scope, v.starts_at, v.expires_at,
  v.internal_note, v.external_note,
  ARRAY(
    SELECT r.resource_key
    FROM killswitch_prescription_version_resources AS r
    WHERE r.organization_id = v.organization_id
      AND r.prescription_id = v.prescription_id
      AND r.version = v.version
    ORDER BY r.resource_key
    LIMIT 1001
  )::text[] AS selected_resource_keys,
  CASE
    WHEN v.state = 'inactive' THEN 'lifted'
    WHEN v.starts_at > clock_timestamp() THEN 'scheduled'
    WHEN v.expires_at IS NOT NULL AND v.expires_at <= clock_timestamp() THEN 'expired'
    ELSE 'active'
  END::text AS customer_status,
  CASE WHEN v.starts_at > v.activated_at THEN 'scheduled' ELSE 'now' END::text AS customer_start,
  COALESCE(a.metadata->>'operation', '')::text AS operation
FROM audit_logs AS a
JOIN killswitch_prescription_versions AS v
  ON v.organization_id = a.organization_id
 AND v.prescription_id = @prescription_id
 AND v.version = COALESCE(
   (a.after_snapshot->>'version')::bigint,
   (a.metadata->>'version')::bigint
 )
WHERE a.organization_id = @organization_id
  AND a.subject_type = 'killswitch_prescription'
  AND a.subject_id = @prescription_id::text
  AND a.action IN ('killswitch:activate', 'killswitch:change', 'killswitch:deactivate', 'killswitch:expire')
ORDER BY a.seq DESC
LIMIT @result_limit;

-- name: ListCustomerKillswitchOverlaps :many
WITH db_time AS (
  SELECT clock_timestamp() AS now
)
SELECT
  p.id, v.resource_scope, v.starts_at, v.expires_at,
  CASE WHEN v.starts_at > db_time.now THEN 'scheduled' ELSE 'active' END::text AS customer_status,
  CASE WHEN v.starts_at > v.activated_at THEN 'scheduled' ELSE 'now' END::text AS customer_start,
  ARRAY(
    SELECT r.resource_key
    FROM killswitch_prescription_version_resources AS r
    WHERE r.organization_id = p.organization_id
      AND r.prescription_id = p.id
      AND r.version = v.version
    ORDER BY r.resource_key
    LIMIT 1001
  )::text[] AS selected_resource_keys
FROM killswitch_prescriptions AS p
JOIN killswitch_prescription_versions AS v
  ON v.organization_id = p.organization_id
 AND v.prescription_id = p.id
 AND v.version = p.current_version
CROSS JOIN db_time
WHERE p.organization_id = @organization_id
  AND p.definition_key = @definition_key
  AND p.principal_kind = @principal_kind
  AND p.principal_key = @principal_key
  AND p.resource_kind = @resource_kind
  AND v.state = 'active'
  AND (v.expires_at IS NULL OR db_time.now < v.expires_at)
  AND (sqlc.narg('exclude_id')::uuid IS NULL OR p.id <> sqlc.narg('exclude_id')::uuid)
  AND @draft_starts_at::timestamptz < COALESCE(v.expires_at, 'infinity'::timestamptz)
  AND v.starts_at < COALESCE(sqlc.narg('draft_ends_at')::timestamptz, 'infinity'::timestamptz)
  AND (
    @draft_scope::text = 'all'
    OR v.resource_scope = 'all'
    OR EXISTS (
      SELECT 1
      FROM killswitch_prescription_version_resources AS r
      WHERE r.organization_id = p.organization_id
        AND r.prescription_id = p.id
        AND r.version = v.version
        AND r.resource_key = ANY(@draft_selected_resource_keys::text[])
    )
  )
ORDER BY v.starts_at ASC, p.id ASC
LIMIT 101;

-- name: BatchCustomerKillswitchUserBadges :many
WITH db_time AS (
  SELECT clock_timestamp() AS now
), requested AS (
  SELECT requested_input.user_id::text AS user_id
  FROM unnest(@user_ids::text[]) AS requested_input(user_id)
)
SELECT
  requested.user_id::text AS user_id,
  EXISTS (
    SELECT 1
    FROM killswitch_prescriptions AS p
    JOIN killswitch_prescription_versions AS v
      ON v.organization_id = p.organization_id
     AND v.prescription_id = p.id
     AND v.version = p.current_version
    CROSS JOIN db_time
    WHERE p.organization_id = @organization_id
      AND p.definition_key = @definition_key
      AND p.principal_kind = @principal_kind
      AND p.principal_key = requested.user_id
      AND p.resource_kind = @resource_kind
      AND v.state = 'active'
      AND v.starts_at <= db_time.now
      AND (v.expires_at IS NULL OR db_time.now < v.expires_at)
  ) AS affected_now,
  EXISTS (
    SELECT 1
    FROM killswitch_prescriptions AS p
    JOIN killswitch_prescription_versions AS v
      ON v.organization_id = p.organization_id
     AND v.prescription_id = p.id
     AND v.version = p.current_version
    CROSS JOIN db_time
    WHERE p.organization_id = @organization_id
      AND p.definition_key = @definition_key
      AND p.principal_kind = @principal_kind
      AND p.principal_key = requested.user_id
      AND p.resource_kind = @resource_kind
      AND v.state = 'active'
      AND v.starts_at > db_time.now
  ) AS scheduled
FROM requested
ORDER BY requested.user_id;
