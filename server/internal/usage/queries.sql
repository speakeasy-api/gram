-- name: GetEnabledServerCount :one
SELECT COUNT(*)
FROM toolsets
WHERE organization_id = @organization_id
  AND mcp_enabled IS TRUE
  AND deleted IS FALSE;

-- name: GetBillingMetadata :one
SELECT *
FROM billing_metadata
WHERE organization_id = @organization_id;

-- name: StoreStripeCustomer :one
INSERT INTO billing_metadata (organization_id, stripe_customer_id)
VALUES (@organization_id, @stripe_customer_id)
ON CONFLICT (organization_id) DO UPDATE SET
    stripe_customer_id = COALESCE(billing_metadata.stripe_customer_id, EXCLUDED.stripe_customer_id)
  , updated_at = CASE
      WHEN billing_metadata.stripe_customer_id IS NULL THEN clock_timestamp()
      ELSE billing_metadata.updated_at
    END
RETURNING *;

-- name: PrepareStripeCheckoutIntent :one
-- Call inside the Checkout transaction after the Stripe customer is stored.
-- The row lock makes concurrent callers reuse one live intent. Once it expires,
-- a caller may replace it only while no subscription has been activated.
WITH locked AS (
  SELECT
      id
    , stripe_checkout_idempotency_key
    , stripe_checkout_billing_cycle_anchor
    , stripe_checkout_trial_end
    , stripe_checkout_expires_at
    , stripe_checkout_session_id
    , (
        stripe_checkout_idempotency_key IS NOT NULL
        AND stripe_checkout_billing_cycle_anchor IS NOT NULL
        AND stripe_checkout_expires_at > sqlc.arg(prepared_at)::timestamptz
      ) AS reuse_existing_intent
  FROM billing_metadata
  WHERE organization_id = sqlc.arg(organization_id)::text
    AND stripe_customer_id = sqlc.arg(stripe_customer_id)::text
  FOR UPDATE
), prepared AS (
  UPDATE billing_metadata AS metadata
  SET
      stripe_checkout_idempotency_key = CASE
        WHEN locked.reuse_existing_intent THEN locked.stripe_checkout_idempotency_key
        ELSE sqlc.arg(stripe_checkout_idempotency_key)::text
      END
    , stripe_checkout_billing_cycle_anchor = CASE
        WHEN locked.reuse_existing_intent THEN locked.stripe_checkout_billing_cycle_anchor
        ELSE sqlc.arg(stripe_checkout_billing_cycle_anchor)::timestamptz
      END
    , stripe_checkout_trial_end = CASE
        WHEN locked.reuse_existing_intent THEN locked.stripe_checkout_trial_end
        ELSE sqlc.narg(stripe_checkout_trial_end)::timestamptz
      END
    , stripe_checkout_expires_at = CASE
        WHEN locked.reuse_existing_intent THEN locked.stripe_checkout_expires_at
        ELSE sqlc.arg(stripe_checkout_expires_at)::timestamptz
      END
    , stripe_checkout_session_id = CASE
        WHEN locked.reuse_existing_intent THEN locked.stripe_checkout_session_id
        ELSE NULL
      END
    , updated_at = CASE
        WHEN locked.reuse_existing_intent THEN metadata.updated_at
        ELSE clock_timestamp()
      END
  FROM locked
  WHERE metadata.id = locked.id
    AND metadata.stripe_subscription_id IS NULL
    -- An expired intent with a known remote session rotates only after the
    -- caller has checked that exact session and explicitly authorizes replacing
    -- it. A sessionless intent has no remote completion race to guard.
    AND (
      locked.reuse_existing_intent
      OR locked.stripe_checkout_session_id IS NULL
      OR locked.stripe_checkout_session_id = sqlc.narg(replace_expired_session_id)::text
    )
  RETURNING
      metadata.id AS billing_metadata_id
    , metadata.stripe_customer_id
    , metadata.stripe_checkout_idempotency_key
    , metadata.stripe_checkout_billing_cycle_anchor
    , metadata.stripe_checkout_trial_end
    , metadata.stripe_checkout_expires_at
    , metadata.stripe_checkout_session_id
    , locked.reuse_existing_intent
)
SELECT
    billing_metadata_id
  , stripe_customer_id
  , stripe_checkout_idempotency_key
  , stripe_checkout_billing_cycle_anchor
  , stripe_checkout_trial_end
  , stripe_checkout_expires_at
  , stripe_checkout_session_id
  , COALESCE(reuse_existing_intent, FALSE)::boolean AS reuse_existing_intent
FROM prepared;

-- name: FinalizeStripeCheckoutIntent :one
-- Attach the remote session only to the exact intent used to create it. A retry
-- may confirm the same session, but cannot replace it with another session ID.
WITH locked AS (
  SELECT
      id
    , stripe_checkout_session_id IS NULL AS attach_new_session
  FROM billing_metadata
  WHERE organization_id = sqlc.arg(organization_id)::text
    AND stripe_customer_id = sqlc.arg(stripe_customer_id)::text
  FOR UPDATE
), finalized AS (
  UPDATE billing_metadata AS metadata
  SET
      stripe_checkout_session_id = sqlc.arg(stripe_checkout_session_id)::text
    , updated_at = CASE
        WHEN locked.attach_new_session THEN clock_timestamp()
        ELSE metadata.updated_at
      END
  FROM locked
  WHERE metadata.id = locked.id
    AND metadata.stripe_subscription_id IS NULL
    AND metadata.stripe_checkout_idempotency_key = sqlc.arg(stripe_checkout_idempotency_key)::text
    AND metadata.stripe_checkout_billing_cycle_anchor = sqlc.arg(stripe_checkout_billing_cycle_anchor)::timestamptz
    AND metadata.stripe_checkout_trial_end IS NOT DISTINCT FROM sqlc.narg(stripe_checkout_trial_end)::timestamptz
    AND metadata.stripe_checkout_expires_at = sqlc.arg(stripe_checkout_expires_at)::timestamptz
    AND (
      metadata.stripe_checkout_session_id IS NULL
      OR metadata.stripe_checkout_session_id = sqlc.arg(stripe_checkout_session_id)::text
    )
  RETURNING
      metadata.id AS billing_metadata_id
    , locked.attach_new_session
)
SELECT
    billing_metadata_id
  , COALESCE(attach_new_session, FALSE)::boolean AS attached_new_session
FROM finalized;

-- name: GetBillingMetadataOrganizationByStripeCustomerID :one
SELECT organization_id
FROM billing_metadata
WHERE stripe_customer_id = @stripe_customer_id;

-- name: TryInsertStripeWebhookReceipt :one
WITH inserted AS (
  INSERT INTO stripe_webhook_receipts (
      stripe_event_id
    , organization_id
    , event_type
  ) VALUES (
      @stripe_event_id
    , @organization_id
    , @event_type
  )
  ON CONFLICT (stripe_event_id) DO NOTHING
  RETURNING stripe_event_id
)
SELECT EXISTS (SELECT 1 FROM inserted) AS inserted;

-- name: StripeWebhookReceiptExists :one
SELECT EXISTS (
    SELECT 1
    FROM stripe_webhook_receipts
    WHERE stripe_event_id = @stripe_event_id
) AS received;

-- name: AcquireStripeSubscriptionActivationLock :exec
-- Serializes distinct Stripe events that refer to the same subscription.
SELECT pg_advisory_xact_lock(hashtextextended(@stripe_subscription_id, 0));

-- name: GetPaygActivationState :one
SELECT
    billing_metadata.id AS billing_metadata_id
  , billing_metadata.stripe_customer_id
  , billing_metadata.stripe_subscription_id
  , billing_metadata.stripe_billing_cycle_anchor
  , billing_metadata.billing_cycle_anchor_day
  , organization_metadata.name AS organization_name
  , organization_metadata.slug AS organization_slug
  , organization_metadata.gram_account_type
  , organization_metadata.whitelisted
FROM billing_metadata
JOIN organization_metadata
  ON organization_metadata.id = billing_metadata.organization_id
WHERE billing_metadata.organization_id = @organization_id
FOR UPDATE OF billing_metadata, organization_metadata;

-- name: ListStripeSubscriptionOwners :many
SELECT organization_id
FROM billing_metadata
WHERE stripe_subscription_id = @stripe_subscription_id
ORDER BY organization_id;

-- name: ActivatePaygBillingMetadata :one
UPDATE billing_metadata
SET stripe_subscription_id = @stripe_subscription_id,
    stripe_billing_cycle_anchor = @stripe_billing_cycle_anchor,
    stripe_checkout_idempotency_key = NULL,
    stripe_checkout_billing_cycle_anchor = NULL,
    stripe_checkout_trial_end = NULL,
    stripe_checkout_expires_at = NULL,
    stripe_checkout_session_id = NULL,
    billing_cycle_anchor_day = @billing_cycle_anchor_day,
    updated_at = clock_timestamp()
WHERE organization_id = @organization_id
  AND stripe_customer_id = @stripe_customer_id
  AND (stripe_subscription_id IS NULL OR stripe_subscription_id = @stripe_subscription_id)
RETURNING *;

-- name: ActivatePaygOrganization :exec
UPDATE organization_metadata
SET gram_account_type = 'payg',
    whitelisted = TRUE,
    updated_at = clock_timestamp()
WHERE id = @organization_id;

-- name: CreateStripeBillingMetadataFixture :exec
-- Test-only fixture for webhook tests that need a Stripe customer association.
INSERT INTO billing_metadata (organization_id, stripe_customer_id)
VALUES (@organization_id, @stripe_customer_id);

-- name: CreateStripeSubscriptionBillingMetadataFixture :exec
-- Test-only fixture for checkout tests that need an existing Stripe subscription.
INSERT INTO billing_metadata (organization_id, stripe_customer_id, stripe_subscription_id)
VALUES (@organization_id, @stripe_customer_id, @stripe_subscription_id);

-- name: SetStripeSubscriptionFixture :exec
-- Test-only fixture for ownership-conflict webhook tests.
UPDATE billing_metadata
SET stripe_subscription_id = @stripe_subscription_id
WHERE organization_id = @organization_id;

-- name: CountStripeWebhookReceiptsFixture :one
-- Test-only fixture assertion for durable webhook completion receipts.
SELECT count(*)
FROM stripe_webhook_receipts
WHERE organization_id = @organization_id;

-- name: UpsertBillingMetadata :one
INSERT INTO billing_metadata (
    organization_id
  , tum_monthly_token_limit
  , alert_email
  , billing_cycle_anchor_day
  , tunneled_mcp_server_limit
) VALUES (
    @organization_id
  , sqlc.narg(tum_monthly_token_limit)
  , sqlc.narg(alert_email)
  , @billing_cycle_anchor_day
  , sqlc.narg(tunneled_mcp_server_limit)
)
ON CONFLICT (organization_id) DO UPDATE SET
    tum_monthly_token_limit = EXCLUDED.tum_monthly_token_limit
  , alert_email = EXCLUDED.alert_email
  , billing_cycle_anchor_day = EXCLUDED.billing_cycle_anchor_day
  -- Omitted (NULL) preserves the configured cap: callers that predate the
  -- field (dashboard TUM form, older SDKs) must not silently clear it.
  , tunneled_mcp_server_limit = COALESCE(EXCLUDED.tunneled_mcp_server_limit, billing_metadata.tunneled_mcp_server_limit)
  , updated_at = clock_timestamp()
RETURNING *;

-- name: GetOrganizationName :one
SELECT name
FROM organization_metadata
WHERE id = @organization_id;

-- name: ListBillingProjectIDsByOrganization :many
-- Intentionally includes soft-deleted projects: usage recorded while a
-- project was live is still billable, and deleting a project mid-cycle must
-- not shrink the cycle's tokens-under-management total.
SELECT id
FROM projects
WHERE organization_id = @organization_id;

-- name: UpsertBillingCycleUsage :exec
INSERT INTO billing_cycle_usage (
    organization_id
  , cycle_start
  , cycle_end
  , tum_tokens
  , finalized_at
) VALUES (
    @organization_id::text
  , @cycle_start
  , @cycle_end
  , @tum_tokens
  , sqlc.narg(finalized_at)
)
ON CONFLICT (organization_id, cycle_start) DO UPDATE SET
    cycle_end = EXCLUDED.cycle_end
  , tum_tokens = EXCLUDED.tum_tokens
  , finalized_at = EXCLUDED.finalized_at
  , updated_at = clock_timestamp()
-- Finalized rows are the permanent billing record and must never be
-- overwritten by later refreshes.
WHERE billing_cycle_usage.finalized_at IS NULL;

-- name: ListFinalizedBillingCycleStarts :many
SELECT cycle_start
FROM billing_cycle_usage
WHERE organization_id = @organization_id::text
  AND finalized_at IS NOT NULL;

-- name: ListBillingCycleUsage :many
SELECT *
FROM billing_cycle_usage
WHERE organization_id = @organization_id::text
ORDER BY cycle_start;
