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
    -- Billing writers lock in advisory-then-row order. Try the same advisory
    -- lock before FOR UPDATE so concurrent batches skip rather than deadlock.
    AND pg_try_advisory_xact_lock(
      hashtextextended('openrouter-' || k.key_type || '-billing:' || k.organization_id, 0)
    )
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
    WHEN om.gram_account_type = 'payg' AND bm.stripe_subscription_id IS NOT NULL THEN 'active'
    WHEN om.gram_account_type = 'free' AND bm.stripe_subscription_id IS NULL AND latest_payg_deactivation.action IS NULL THEN 'irrelevant'
    WHEN om.gram_account_type = 'free' AND bm.stripe_subscription_id IS NULL
      AND jsonb_typeof(latest_payg_deactivation.before_snapshot) = 'object'
      AND jsonb_typeof(latest_payg_deactivation.after_snapshot) = 'object'
      AND latest_payg_deactivation.before_snapshot ->> 'account_type' = 'payg'
      AND latest_payg_deactivation.after_snapshot ->> 'account_type' = 'free' THEN 'inactive'
    WHEN om.gram_account_type IN ('free', 'payg') THEN 'inconsistent'
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
  SELECT a.action, a.before_snapshot, a.after_snapshot
  FROM audit_logs AS a
  WHERE a.organization_id = k.organization_id
    AND a.subject_id = k.organization_id
    AND a.subject_type = 'organization'
    AND a.action = 'organization:payg_deactivated'
  ORDER BY a.seq DESC
  LIMIT 1
) AS latest_payg_deactivation ON TRUE
LEFT JOIN LATERAL (
  SELECT a.action, a.metadata, a.before_snapshot, a.after_snapshot
  FROM audit_logs AS a
  WHERE a.organization_id = k.organization_id
    AND a.subject_id = k.organization_id || '/' || k.key_type
    AND a.subject_type = 'openrouter_api_key'
    AND a.action IN ('openrouter-key:disable', 'openrouter-key:enable')
  ORDER BY a.seq DESC
  LIMIT 1
) AS latest_admin ON TRUE
ORDER BY k.organization_id, k.key_type;

-- name: AcquireOpenRouterBillingLock :exec
SELECT pg_advisory_xact_lock(
    hashtextextended('openrouter-' || @key_type::text || '-billing:' || @organization_id::text, 0)
);

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
    WHEN t.ends_at > CURRENT_TIMESTAMP THEN 'contradictory'
    ELSE 'demoted'
  END AS trial_state,
  CASE
    WHEN k.key_type <> 'chat' THEN 'irrelevant'
    WHEN om.gram_account_type = 'payg' AND bm.stripe_subscription_id IS NOT NULL THEN 'active'
    WHEN om.gram_account_type = 'free' AND bm.stripe_subscription_id IS NULL AND latest_payg_deactivation.action IS NULL THEN 'irrelevant'
    WHEN om.gram_account_type = 'free' AND bm.stripe_subscription_id IS NULL
      AND jsonb_typeof(latest_payg_deactivation.before_snapshot) = 'object'
      AND jsonb_typeof(latest_payg_deactivation.after_snapshot) = 'object'
      AND latest_payg_deactivation.before_snapshot ->> 'account_type' = 'payg'
      AND latest_payg_deactivation.after_snapshot ->> 'account_type' = 'free' THEN 'inactive'
    WHEN om.gram_account_type IN ('free', 'payg') THEN 'inconsistent'
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
  SELECT a.action, a.before_snapshot, a.after_snapshot
  FROM audit_logs AS a
  WHERE a.organization_id = k.organization_id
    AND a.subject_id = k.organization_id
    AND a.subject_type = 'organization'
    AND a.action = 'organization:payg_deactivated'
  ORDER BY a.seq DESC
  LIMIT 1
) AS latest_payg_deactivation ON TRUE
LEFT JOIN LATERAL (
  SELECT a.action, a.metadata, a.before_snapshot, a.after_snapshot
  FROM audit_logs AS a
  WHERE a.organization_id = k.organization_id
    AND a.subject_id = k.organization_id || '/' || k.key_type
    AND a.subject_type = 'openrouter_api_key'
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

-- TEST FIXTURE ONLY: every query below is for package tests and may create impossible states or take exclusive locks.
-- Non-test application code must never call these generated methods.

-- name: SeedOrganizationFixture :exec
INSERT INTO organization_metadata (id, name, slug, gram_account_type)
VALUES (@organization_id, 'test', @organization_id, @account_type);

-- name: SeedOpenRouterKeyFixture :exec
INSERT INTO openrouter_api_keys (organization_id, key_type, key_hash, disabled, disable_causes)
VALUES (@organization_id, @key_type, 'test-hash', @disabled, NULL);

-- name: GetOpenRouterDisableCausesFixture :one
SELECT disable_causes
FROM openrouter_api_keys
WHERE organization_id = @organization_id AND key_type = @key_type;

-- name: SeedTrialFixture :exec
INSERT INTO trials (organization_id, tier, ends_at, demoted_at, converted_at)
VALUES (@organization_id, 'enterprise', @ends_at, sqlc.narg('demoted_at'), sqlc.narg('converted_at'));

-- name: SeedBillingFixture :exec
INSERT INTO billing_metadata (organization_id, stripe_subscription_id)
VALUES (@organization_id, sqlc.narg('stripe_subscription_id'));

-- name: SeedAdminAuditFixture :exec
INSERT INTO audit_logs (
  organization_id, actor_id, actor_type, action, subject_id, subject_type,
  before_snapshot, after_snapshot, metadata
)
VALUES (
  @organization_id, 'system:test', 'system', @action, @subject_id, 'openrouter_api_key',
  @before_snapshot, @after_snapshot, @metadata
);

-- name: SeedAuditLogFixture :exec
INSERT INTO audit_logs (
  organization_id, actor_id, actor_type, action, subject_id, subject_type,
  before_snapshot, after_snapshot, metadata
)
VALUES (
  @organization_id, 'system:test', 'system', @action, @subject_id, @subject_type,
  @before_snapshot, @after_snapshot, @metadata
);

-- name: SetOpenRouterClassificationFixture :exec
UPDATE openrouter_api_keys
SET disable_causes = @disable_causes::text[], disabled = @disabled
WHERE organization_id = @organization_id AND key_type = @key_type;

-- name: TouchOpenRouterClassificationFixture :exec
UPDATE openrouter_api_keys
SET updated_at = GREATEST(clock_timestamp(), updated_at + INTERVAL '1 microsecond')
WHERE organization_id = @organization_id AND key_type = @key_type;

-- name: ResetOpenRouterClassificationFixture :exec
UPDATE openrouter_api_keys
SET disable_causes = NULL
WHERE organization_id = @organization_id AND key_type = @key_type;

-- name: CountAllNullClassificationsFixture :one
SELECT count(*)
FROM openrouter_api_keys
WHERE disable_causes IS NULL;

-- name: LockAuditLogsFixture :exec
LOCK TABLE audit_logs IN ACCESS EXCLUSIVE MODE;

-- name: LockOpenRouterKeysFixture :exec
LOCK TABLE openrouter_api_keys IN ACCESS EXCLUSIVE MODE;
