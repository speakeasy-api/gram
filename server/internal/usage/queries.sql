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
