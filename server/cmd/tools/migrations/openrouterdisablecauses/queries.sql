-- name: SetLocalTimeouts :one
SELECT
  set_config('lock_timeout', @lock_timeout::text, true) AS lock_timeout,
  set_config('statement_timeout', @statement_timeout::text, true) AS statement_timeout;

-- name: LockClassificationBatch :many
WITH candidates AS (
  SELECT k.organization_id, k.key_type
  FROM openrouter_api_keys AS k
  WHERE k.disable_causes IS NULL
    AND k.deleted IS FALSE
    AND (k.organization_id, k.key_type) > (@after_organization_id::text, @after_key_type::text)
  ORDER BY k.organization_id, k.key_type
  LIMIT @batch_size::int
  FOR UPDATE OF k SKIP LOCKED
)
SELECT
  k.organization_id,
  k.key_type,
  k.disabled AS legacy_disabled,
  CASE
    WHEN t.demoted_at IS NULL THEN 'none'
    WHEN t.converted_at IS NOT NULL AND t.converted_at < t.demoted_at THEN 'contradictory'
    WHEN t.ends_at > clock_timestamp() THEN 'contradictory'
    ELSE 'demoted'
  END AS trial_state,
  CASE
    WHEN k.key_type <> 'chat' THEN 'irrelevant'
    WHEN om.gram_account_type = 'base' AND bm.stripe_subscription_id IS NULL THEN 'inactive'
    WHEN om.gram_account_type = 'payg' AND bm.stripe_subscription_id IS NOT NULL THEN 'active'
    WHEN om.gram_account_type IN ('base', 'payg') THEN 'inconsistent'
    ELSE 'irrelevant'
  END AS billing_state,
  COALESCE(latest_admin.action, '') AS admin_action,
  latest_admin.metadata AS admin_metadata,
  latest_admin.before_snapshot AS admin_before_snapshot,
  latest_admin.after_snapshot AS admin_after_snapshot
FROM candidates AS c
JOIN openrouter_api_keys AS k
  ON k.organization_id = c.organization_id AND k.key_type = c.key_type
JOIN organization_metadata AS om ON om.id = k.organization_id
LEFT JOIN trials AS t ON t.organization_id = k.organization_id
LEFT JOIN billing_metadata AS bm ON bm.organization_id = k.organization_id
LEFT JOIN LATERAL (
  SELECT a.action, a.metadata, a.before_snapshot, a.after_snapshot
  FROM audit_logs AS a
  WHERE a.organization_id = k.organization_id
    AND a.subject_id = 'openrouter_api_key:' || k.organization_id || '/' || k.key_type
    AND a.action IN ('openrouter-key:disable', 'openrouter-key:enable')
  ORDER BY a.seq DESC
  LIMIT 1
) AS latest_admin ON TRUE
ORDER BY k.organization_id, k.key_type;

-- name: CompareAndSetClassification :execrows
UPDATE openrouter_api_keys
SET disable_causes = @disable_causes::text[],
    disabled = cardinality(@disable_causes::text[]) > 0,
    updated_at = GREATEST(clock_timestamp(), updated_at + INTERVAL '1 microsecond')
WHERE organization_id = @organization_id
  AND key_type = @key_type
  AND disable_causes IS NULL
  AND deleted IS FALSE;

-- name: CountLiveNullClassifications :one
SELECT count(*)
FROM openrouter_api_keys
WHERE disable_causes IS NULL AND deleted IS FALSE;

-- name: CountDeletedNullClassifications :one
SELECT count(*)
FROM openrouter_api_keys
WHERE disable_causes IS NULL AND deleted IS TRUE;

-- name: ListValidationBatch :many
SELECT
  k.organization_id,
  k.key_type,
  k.disabled AS legacy_disabled,
  k.disable_causes,
  CASE
    WHEN t.demoted_at IS NULL THEN 'none'
    WHEN t.converted_at IS NOT NULL AND t.converted_at < t.demoted_at THEN 'contradictory'
    WHEN t.ends_at > clock_timestamp() THEN 'contradictory'
    ELSE 'demoted'
  END AS trial_state,
  CASE
    WHEN k.key_type <> 'chat' THEN 'irrelevant'
    WHEN om.gram_account_type = 'base' AND bm.stripe_subscription_id IS NULL THEN 'inactive'
    WHEN om.gram_account_type = 'payg' AND bm.stripe_subscription_id IS NOT NULL THEN 'active'
    WHEN om.gram_account_type IN ('base', 'payg') THEN 'inconsistent'
    ELSE 'irrelevant'
  END AS billing_state,
  COALESCE(latest_admin.action, '') AS admin_action,
  latest_admin.metadata AS admin_metadata,
  latest_admin.before_snapshot AS admin_before_snapshot,
  latest_admin.after_snapshot AS admin_after_snapshot
FROM openrouter_api_keys AS k
JOIN organization_metadata AS om ON om.id = k.organization_id
LEFT JOIN trials AS t ON t.organization_id = k.organization_id
LEFT JOIN billing_metadata AS bm ON bm.organization_id = k.organization_id
LEFT JOIN LATERAL (
  SELECT a.action, a.metadata, a.before_snapshot, a.after_snapshot
  FROM audit_logs AS a
  WHERE a.organization_id = k.organization_id
    AND a.subject_id = 'openrouter_api_key:' || k.organization_id || '/' || k.key_type
    AND a.action IN ('openrouter-key:disable', 'openrouter-key:enable')
  ORDER BY a.seq DESC
  LIMIT 1
) AS latest_admin ON TRUE
WHERE k.deleted IS FALSE
  AND (k.organization_id, k.key_type) > (@after_organization_id::text, @after_key_type::text)
ORDER BY k.organization_id, k.key_type
LIMIT @batch_size::int;

-- name: GetManualOverrideTarget :one
SELECT disabled, disable_causes
FROM openrouter_api_keys
WHERE organization_id = @organization_id
  AND key_type = @key_type
  AND deleted IS FALSE
FOR UPDATE;
