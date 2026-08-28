-- name: LockOpenRouterKeyProvisioning :exec
-- Serialize first-time key creation per (org, key type) so concurrent
-- completions cannot both mint an upstream OpenRouter key.
SELECT pg_advisory_xact_lock(hashtext('openrouter_key:' || @organization_id::text || ':' || @key_type::text));

-- name: AcquireOpenRouterKeyBillingLock :exec
SELECT pg_advisory_lock(
    hashtextextended('openrouter-' || @key_type::text || '-billing:' || @organization_id::text, 0)
);

-- name: ReleaseOpenRouterKeyBillingLock :one
SELECT pg_advisory_unlock(
    hashtextextended('openrouter-' || @key_type::text || '-billing:' || @organization_id::text, 0)
) AS unlocked;

-- name: CreateOpenRouterAPIKey :one
INSERT INTO openrouter_api_keys (
    organization_id
  , key_type
  , key_encrypted
  , key_hash
  , monthly_credits
  , disable_causes
) VALUES (
    @organization_id
  , @key_type
  , @key_encrypted
  , @key_hash
  , @monthly_credits
  , '{}'::text[]
)
RETURNING *;

-- name: AcquireOpenRouterBillingLock :exec
-- Shared with billing reconciliation and the disable-cause classifier.
SELECT pg_advisory_xact_lock(
    hashtextextended('openrouter-' || @key_type::text || '-billing:' || @organization_id::text, 0)
);

-- name: GetOpenRouterAPIKey :one
SELECT *
FROM openrouter_api_keys
WHERE organization_id = @organization_id
  AND key_type = @key_type
  AND deleted IS FALSE;

-- name: PrepareEnterpriseTrialConversionKey :one
-- Local-only conversion preparation. The caller owns the transaction and has
-- already acquired lifecycle and per-key advisory locks in canonical order.
WITH existing AS MATERIALIZED (
  SELECT keys.key_type, keys.monthly_credits, keys.disabled, keys.disable_causes
  FROM openrouter_api_keys AS keys
  WHERE keys.organization_id = @organization_id
    AND keys.key_type = @key_type
    AND keys.deleted IS FALSE
), desired AS (
  SELECT
    key_type,
    GREATEST(monthly_credits, @enterprise_floor::bigint) AS monthly_credits,
    CASE
      WHEN key_type = 'chat' THEN array_remove(array_remove(disable_causes, 'trial_demotion'), 'billing_inactive')
      ELSE array_remove(disable_causes, 'trial_demotion')
    END AS disable_causes
  FROM existing
), updated AS (
  UPDATE openrouter_api_keys AS keys
  SET monthly_credits = desired.monthly_credits,
      disable_causes = desired.disable_causes,
      disabled = cardinality(desired.disable_causes) > 0,
      updated_at = CASE
        WHEN keys.monthly_credits IS DISTINCT FROM desired.monthly_credits
          OR keys.disable_causes IS DISTINCT FROM desired.disable_causes
          OR keys.disabled IS DISTINCT FROM (cardinality(desired.disable_causes) > 0)
          THEN GREATEST(clock_timestamp(), keys.updated_at + INTERVAL '1 microsecond')
        ELSE keys.updated_at
      END
  FROM desired
  WHERE keys.organization_id = @organization_id
    AND keys.key_type = @key_type
    AND keys.deleted IS FALSE
    AND desired.disable_causes IS NOT NULL
  RETURNING keys.monthly_credits, keys.disabled, keys.disable_causes
)
SELECT
  existing.key_type,
  (existing.disable_causes IS NOT NULL)::boolean AS classified,
  existing.monthly_credits AS before_monthly_credits,
  existing.disabled AS before_disabled,
  existing.disable_causes AS before_disable_causes,
  updated.monthly_credits AS after_monthly_credits,
  updated.disabled AS after_disabled,
  updated.disable_causes AS after_disable_causes
FROM existing
LEFT JOIN updated ON TRUE;

-- name: UpdateOpenRouterKey :one
UPDATE openrouter_api_keys
SET monthly_credits = @monthly_credits, key_hash = @key_hash,
    disabled = CASE
      WHEN @reinstate::boolean AND disable_causes IS NULL THEN FALSE
      ELSE disabled
    END,
    disable_causes = CASE
      WHEN @reinstate::boolean AND disable_causes IS NULL THEN '{}'::text[]
      ELSE disable_causes
    END,
    updated_at = GREATEST(clock_timestamp(), updated_at + INTERVAL '1 microsecond')
WHERE organization_id = @organization_id
  AND key_type = @key_type
  AND deleted IS FALSE
RETURNING *;

-- name: AddOpenRouterAPIKeyDisableCause :one
UPDATE openrouter_api_keys
SET disable_causes = CASE
      WHEN @disable_cause::text = ANY(disable_causes) THEN disable_causes
      ELSE ARRAY(
        SELECT cause
        FROM unnest(array_append(disable_causes, @disable_cause::text)) AS causes(cause)
        GROUP BY cause
        ORDER BY CASE cause
          WHEN 'admin_lock' THEN 1
          WHEN 'trial_demotion' THEN 2
          WHEN 'billing_inactive' THEN 3
          ELSE 4
        END
      )
    END,
    disabled = TRUE,
    updated_at = CASE
      WHEN @disable_cause::text = ANY(disable_causes) THEN updated_at
      ELSE GREATEST(clock_timestamp(), updated_at + INTERVAL '1 microsecond')
    END
WHERE organization_id = @organization_id
  AND key_type = @key_type
  AND key_hash = @key_hash
  AND disable_causes IS NOT NULL
  AND deleted IS FALSE
RETURNING *;

-- name: RemoveOpenRouterAPIKeyDisableCause :one
UPDATE openrouter_api_keys
SET disable_causes = CASE
      WHEN @disable_cause::text = ANY(disable_causes) THEN ARRAY(
        SELECT cause
        FROM unnest(array_remove(disable_causes, @disable_cause::text)) AS causes(cause)
        GROUP BY cause
        ORDER BY CASE cause
          WHEN 'admin_lock' THEN 1
          WHEN 'trial_demotion' THEN 2
          WHEN 'billing_inactive' THEN 3
          ELSE 4
        END, cause
      )
      ELSE disable_causes
    END,
    disabled = CASE
      WHEN @disable_cause::text = ANY(disable_causes)
        AND cardinality(array_remove(disable_causes, @disable_cause::text)) = 0 THEN FALSE
      ELSE disabled
    END,
    monthly_credits = CASE
      WHEN @disable_cause::text = ANY(disable_causes) AND @update_monthly_credits::boolean
        THEN @monthly_credits::bigint
      ELSE monthly_credits
    END,
    updated_at = CASE
      WHEN @disable_cause::text = ANY(disable_causes)
        THEN GREATEST(clock_timestamp(), updated_at + INTERVAL '1 microsecond')
      ELSE updated_at
    END
WHERE organization_id = @organization_id
  AND key_type = @key_type
  AND key_hash = @key_hash
  AND disable_causes IS NOT NULL
  AND deleted IS FALSE
RETURNING *;

-- name: DisableOpenRouterAPIKey :exec
-- Locks the key down without deleting it, so a reinstated organization keeps
-- the same upstream key and its ceiling. ProvisionAPIKey reads this flag and
-- refuses to hand the key to a completion.
UPDATE openrouter_api_keys
SET disabled = TRUE,
    disable_causes = NULL,
    updated_at = clock_timestamp()
WHERE organization_id = @organization_id
  AND key_type = @key_type
  AND deleted IS FALSE;

-- name: UpdateOpenRouterKeyMonthlyCredits :exec
-- Updates only monthly_credits for the given organization. Used by the
-- metrics-collection reconciliation path when the upstream OpenRouter limit
-- diverges from the locally cached value (e.g. after a manual change on the
-- OpenRouter dashboard). Distinct from UpdateOpenRouterKey, which is the
-- key-provisioning write path and also mutates key_hash.
UPDATE openrouter_api_keys
SET monthly_credits = @monthly_credits,
    updated_at = GREATEST(clock_timestamp(), updated_at + INTERVAL '1 microsecond')
WHERE organization_id = @organization_id
  AND key_type = @key_type
  AND deleted IS FALSE;

-- name: CompareAndSetOpenRouterKeyMonthlyCredits :execrows
-- Reconciles an upstream observation only while the local mirror still equals
-- what the caller observed. A concurrent explicit cap change wins this CAS.
UPDATE openrouter_api_keys
SET monthly_credits = @monthly_credits,
    updated_at = GREATEST(clock_timestamp(), updated_at + INTERVAL '1 microsecond')
WHERE organization_id = @organization_id
  AND key_type = @key_type
  AND monthly_credits = @current_monthly_credits
  AND (extract(epoch FROM updated_at) * 1000000)::bigint = @current_generation::bigint
  AND deleted IS FALSE;
