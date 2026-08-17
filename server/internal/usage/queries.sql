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

-- name: ListMaterializedOpenRouterInferenceKeys :many
SELECT key_type, disabled
FROM openrouter_api_keys
WHERE organization_id = @organization_id
  AND key_type = ANY(@key_types::text[])
  AND deleted IS FALSE
ORDER BY key_type;

-- name: GetMaterializedOpenRouterInferenceKey :one
SELECT key_type, disabled
FROM openrouter_api_keys
WHERE organization_id = @organization_id
  AND key_type = @key_type
  AND deleted IS FALSE;

-- name: GetPaygBillingSummaryCosts :one
WITH inputs AS (
  SELECT
      sqlc.arg(tum_tokens)::bigint AS tum_tokens
    , sqlc.arg(tum_unit_price_usd)::text::numeric(20, 8) AS tum_unit_price_usd
), completed_spend AS (
  SELECT
      COALESCE(SUM(spend_usd), 0)::numeric(30, 6) AS chat_spend_usd
    , MAX(day)::date AS recorded_through
  FROM openrouter_spend_daily
  WHERE organization_id = sqlc.arg(organization_id)::text
    AND key_type = 'chat'
    AND day >= (sqlc.arg(period_start)::timestamptz AT TIME ZONE 'UTC')::date
    AND day < (sqlc.arg(period_end)::timestamptz AT TIME ZONE 'UTC')::date
    AND day < (sqlc.arg(completed_before)::timestamptz AT TIME ZONE 'UTC')::date
)
SELECT
    inputs.tum_unit_price_usd::text AS tum_unit_price_usd
  , (inputs.tum_tokens::numeric * inputs.tum_unit_price_usd)::numeric(30, 8)::text AS tum_cost_usd
  , completed_spend.chat_spend_usd::text AS chat_spend_usd
  , completed_spend.recorded_through
  , (inputs.tum_tokens::numeric * inputs.tum_unit_price_usd + completed_spend.chat_spend_usd)::numeric(30, 8)::text AS estimated_total_usd
FROM inputs
CROSS JOIN completed_spend;

-- name: LockBillingMetadata :one
SELECT *
FROM billing_metadata
WHERE organization_id = @organization_id
FOR UPDATE;

-- name: LockBillingMetadataOrganization :exec
-- Serializes absent-row first writes and all billing metadata updates.
SELECT pg_advisory_xact_lock(hashtextextended('billing-email:' || @organization_id::text, 0));

-- name: UpsertBillingEmail :one
INSERT INTO billing_metadata (organization_id, alert_email)
VALUES (@organization_id, sqlc.narg(alert_email))
ON CONFLICT (organization_id) DO UPDATE SET
    alert_email = EXCLUDED.alert_email
  , updated_at = clock_timestamp()
RETURNING *;

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

-- name: AcquireOpenRouterBillingLock :exec
-- Serializes one platform inference key with cap writes/reconciliation. The
-- caller acquires every platform key in the order defined by AllKeyTypes.
SELECT pg_advisory_xact_lock(
    hashtextextended('openrouter-' || @key_type::text || '-billing:' || @organization_id::text, 0)
);

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

-- name: DeactivatePaygBillingMetadata :execrows
UPDATE billing_metadata
SET stripe_subscription_id = NULL,
    stripe_billing_cycle_anchor = NULL,
    updated_at = clock_timestamp()
WHERE organization_id = @organization_id
  AND stripe_customer_id = @stripe_customer_id
  AND stripe_subscription_id = @stripe_subscription_id;

-- name: DeactivatePaygOrganization :execrows
UPDATE organization_metadata
SET gram_account_type = 'free',
    whitelisted = FALSE,
    updated_at = clock_timestamp()
WHERE id = @organization_id
  AND gram_account_type = 'payg';

-- name: DisablePaygOpenRouterChatKey :exec
UPDATE openrouter_api_keys
SET disabled = TRUE,
    updated_at = clock_timestamp()
WHERE organization_id = @organization_id
  AND key_type = 'chat'
  AND deleted IS FALSE;

-- name: CreateStripeBillingMetadataFixture :exec
-- Test-only fixture for webhook tests that need a Stripe customer association.
INSERT INTO billing_metadata (organization_id, stripe_customer_id)
VALUES (@organization_id, @stripe_customer_id);

-- name: CreateStripeSubscriptionBillingMetadataFixture :exec
-- Test-only fixture for checkout tests that need an existing Stripe subscription.
INSERT INTO billing_metadata (organization_id, stripe_customer_id, stripe_subscription_id)
VALUES (@organization_id, @stripe_customer_id, @stripe_subscription_id);

-- name: UpsertOpenRouterDailySpendFixture :exec
-- Test-only fixture for billing-summary reads.
INSERT INTO openrouter_spend_daily (organization_id, key_type, day, spend_usd)
VALUES (@organization_id, 'chat', @day, @spend_usd)
ON CONFLICT (organization_id, key_type, day) DO UPDATE
SET spend_usd = EXCLUDED.spend_usd,
    updated_at = clock_timestamp();

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

-- name: GetTUMMeteringOrganization :one
SELECT
    billing_metadata.organization_id
  , billing_metadata.stripe_customer_id
  , billing_metadata.stripe_subscription_id
  , billing_metadata.stripe_billing_cycle_anchor
  , organization_metadata.gram_account_type
FROM billing_metadata
JOIN organization_metadata
  ON organization_metadata.id = billing_metadata.organization_id
WHERE billing_metadata.organization_id = @organization_id;

-- name: ListTUMBillingCyclesForReporting :many
SELECT *
FROM billing_cycle_usage
WHERE organization_id = @organization_id
  AND cycle_start >= @first_paid_cycle_start
ORDER BY cycle_start;

-- name: FreezeTUMBillingCycleBaseline :one
UPDATE billing_cycle_usage
SET billed_tum_tokens = tum_tokens,
    billed_frozen_at = @frozen_at,
    updated_at = clock_timestamp()
WHERE organization_id = @organization_id
  AND id = @billing_cycle_usage_id
  AND billed_tum_tokens IS NULL
  AND billed_frozen_at IS NULL
RETURNING *;

-- name: FreezeMissedTUMBillingCycleBaseline :one
-- If reporting was unavailable for the entire +48h..+72h window, the closed
-- invoice received no immutable baseline. Record zero as billed so the full
-- finalized usage becomes one carry-forward allocation instead of disappearing.
UPDATE billing_cycle_usage
SET billed_tum_tokens = 0,
    billed_frozen_at = @frozen_at,
    updated_at = clock_timestamp()
WHERE organization_id = @organization_id
  AND id = @billing_cycle_usage_id
  AND billed_tum_tokens IS NULL
  AND billed_frozen_at IS NULL
  AND finalized_at IS NOT NULL
RETURNING *;

-- name: CreateTUMMeterReportIntent :one
WITH locked_cycle AS (
  SELECT id, organization_id, cycle_start, cycle_end
  FROM billing_cycle_usage
  WHERE billing_cycle_usage.organization_id = @organization_id
    AND billing_cycle_usage.id = @billing_cycle_usage_id
  FOR UPDATE
), report_totals AS (
  SELECT
      COALESCE(MAX(stripe_meter_reports.seq), 0)::int AS max_seq
    , COALESCE(SUM(stripe_meter_reports.delta_tokens) FILTER (
        WHERE stripe_meter_reports.delivery_state IN ('pending', 'ambiguous', 'confirmed')
      ), 0)::bigint AS intended_tokens
  FROM locked_cycle
  LEFT JOIN stripe_meter_reports
    ON stripe_meter_reports.organization_id = locked_cycle.organization_id
   AND stripe_meter_reports.cycle_start = locked_cycle.cycle_start
), intended AS (
  SELECT
      locked_cycle.*
    , report_totals.max_seq + 1 AS next_seq
    , sqlc.arg(target_tum_tokens)::bigint - report_totals.intended_tokens AS delta_tokens
  FROM locked_cycle
  CROSS JOIN report_totals
)
INSERT INTO stripe_meter_reports (
    organization_id
  , billing_cycle_usage_id
  , cycle_start
  , cycle_end
  , seq
  , stripe_customer_id
  , stripe_meter_event_name
  , stripe_identifier
  , delta_tokens
  , event_timestamp
  , delivery_state
)
SELECT
    intended.organization_id
  , intended.id
  , intended.cycle_start
  , intended.cycle_end
  , intended.next_seq
  , @stripe_customer_id
  , @stripe_meter_event_name
  , 'tum:' || intended.organization_id || ':' || to_char(intended.cycle_start AT TIME ZONE 'UTC', 'YYYY-MM-DD"T"HH24:MI:SS"Z"') || ':' || intended.next_seq::text
  , intended.delta_tokens
  , @event_timestamp
  , 'pending'
FROM intended
WHERE intended.delta_tokens <> 0
RETURNING *;

-- name: ListTUMMeterReportsForDelivery :many
SELECT *
FROM stripe_meter_reports
WHERE organization_id = @organization_id
  AND delivery_state IN ('pending', 'ambiguous')
  AND billing_cycle_usage_id IS NOT NULL
  AND cycle_end IS NOT NULL
  AND stripe_customer_id IS NOT NULL
  AND stripe_meter_event_name IS NOT NULL
  AND stripe_identifier IS NOT NULL
  AND event_timestamp IS NOT NULL
  AND (first_attempted_at IS NULL OR first_attempted_at > @retry_after)
ORDER BY cycle_start, seq
LIMIT 1;

-- name: BeginTUMMeterReportAttempt :one
UPDATE stripe_meter_reports
SET first_attempted_at = COALESCE(first_attempted_at, @attempted_at),
    last_attempted_at = @attempted_at,
    updated_at = clock_timestamp()
WHERE organization_id = @organization_id
  AND id = @id
  AND delivery_state IN ('pending', 'ambiguous')
  AND (first_attempted_at IS NULL OR first_attempted_at > @retry_after)
RETURNING *;

-- name: ConfirmTUMMeterReport :execrows
UPDATE stripe_meter_reports
SET delivery_state = 'confirmed',
    confirmed_at = @confirmed_at,
    updated_at = clock_timestamp()
WHERE organization_id = @organization_id
  AND id = @id
  AND delivery_state IN ('pending', 'ambiguous');

-- name: MarkTUMMeterReportAmbiguous :execrows
UPDATE stripe_meter_reports
SET delivery_state = 'ambiguous',
    ambiguous_at = COALESCE(ambiguous_at, @ambiguous_at),
    updated_at = clock_timestamp()
WHERE organization_id = @organization_id
  AND id = @id
  AND delivery_state IN ('pending', 'ambiguous');

-- name: ListStaleTUMMeterReportCycles :many
SELECT
    stripe_meter_reports.billing_cycle_usage_id
  , stripe_meter_reports.cycle_start
  , stripe_meter_reports.cycle_end
  , stripe_meter_reports.stripe_customer_id
  , MIN(stripe_meter_reports.reconciled_at)::timestamptz AS absence_observed_at
FROM stripe_meter_reports
WHERE organization_id = @organization_id
  AND delivery_state IN ('pending', 'ambiguous')
  AND billing_cycle_usage_id IS NOT NULL
  AND cycle_end IS NOT NULL
  AND stripe_customer_id IS NOT NULL
GROUP BY
    stripe_meter_reports.billing_cycle_usage_id
  , stripe_meter_reports.cycle_start
  , stripe_meter_reports.cycle_end
  , stripe_meter_reports.stripe_customer_id
HAVING bool_and(
  stripe_meter_reports.first_attempted_at IS NOT NULL
  AND stripe_meter_reports.first_attempted_at <= @retry_after
)
ORDER BY cycle_start
LIMIT 1;

-- name: GetTUMMeterReportTotals :one
SELECT
    COALESCE(SUM(delta_tokens) FILTER (WHERE delivery_state = 'confirmed'), 0)::bigint AS confirmed_tokens
  , COALESCE(SUM(delta_tokens) FILTER (WHERE delivery_state IN ('pending', 'ambiguous', 'confirmed')), 0)::bigint AS intended_tokens
FROM stripe_meter_reports
WHERE organization_id = @organization_id
  AND billing_cycle_usage_id = @billing_cycle_usage_id;

-- name: ConfirmReconciledTUMMeterReports :execrows
UPDATE stripe_meter_reports
SET delivery_state = 'confirmed',
    confirmed_at = COALESCE(confirmed_at, @reconciled_at),
    reconciled_at = @reconciled_at,
    updated_at = clock_timestamp()
WHERE organization_id = @organization_id
  AND billing_cycle_usage_id = @billing_cycle_usage_id
  AND delivery_state IN ('pending', 'ambiguous')
  AND first_attempted_at IS NOT NULL
  AND first_attempted_at <= @retry_after;

-- name: MarkReconciledTUMMeterReportsMissing :execrows
UPDATE stripe_meter_reports
SET delivery_state = 'reconciled_missing',
    reconciled_at = @reconciled_at,
    updated_at = clock_timestamp()
WHERE organization_id = @organization_id
  AND billing_cycle_usage_id = @billing_cycle_usage_id
  AND delivery_state IN ('pending', 'ambiguous')
  AND first_attempted_at IS NOT NULL
  AND first_attempted_at <= @retry_after;

-- name: NoteTUMMeterReportReconciliation :execrows
UPDATE stripe_meter_reports
SET reconciled_at = @reconciled_at,
    updated_at = clock_timestamp()
WHERE organization_id = @organization_id
  AND billing_cycle_usage_id = @billing_cycle_usage_id
  AND delivery_state IN ('pending', 'ambiguous')
  AND first_attempted_at IS NOT NULL
  AND first_attempted_at <= @retry_after;

-- name: CreateTUMCarryAllocation :execrows
INSERT INTO stripe_invoice_allocations (
    organization_id
  , source_kind
  , source_key
  , seq
  , source_period_start
  , source_period_end
  , source_snapshot_usd
  , delta_tokens
  , original_tum_unit_price_usd
  , amount_usd
  , idempotency_key
  , delivery_state
)
SELECT
    billing_cycle_usage.organization_id
  , 'tum_cycle'
  , extract(epoch FROM billing_cycle_usage.cycle_start)::bigint::text || ':' || extract(epoch FROM billing_cycle_usage.cycle_end)::bigint::text
  , 1
  , billing_cycle_usage.cycle_start
  , billing_cycle_usage.cycle_end
  , round(billing_cycle_usage.tum_tokens::numeric * sqlc.arg(tum_unit_price_usd)::text::numeric, 6)
  , billing_cycle_usage.tum_tokens - billing_cycle_usage.billed_tum_tokens
  , sqlc.arg(tum_unit_price_usd)::text::numeric
  , round(billing_cycle_usage.tum_tokens::numeric * sqlc.arg(tum_unit_price_usd)::text::numeric, 2)
    - round(billing_cycle_usage.billed_tum_tokens::numeric * sqlc.arg(tum_unit_price_usd)::text::numeric, 2)
  , 'tum-carry:' || billing_cycle_usage.organization_id || ':' || extract(epoch FROM billing_cycle_usage.cycle_start)::bigint::text
  , 'pending'
FROM billing_cycle_usage
WHERE billing_cycle_usage.organization_id = @organization_id
  AND billing_cycle_usage.id = @billing_cycle_usage_id
  AND billing_cycle_usage.billed_tum_tokens IS NOT NULL
  AND billing_cycle_usage.finalized_at IS NOT NULL
  AND billing_cycle_usage.tum_tokens <> billing_cycle_usage.billed_tum_tokens
ON CONFLICT (organization_id, source_kind, source_key, seq) DO NOTHING;

-- name: ListTUMMeterReportsFixture :many
SELECT *
FROM stripe_meter_reports
WHERE organization_id = @organization_id
ORDER BY cycle_start, seq;

-- name: CreateLegacyTUMMeterReportFixture :one
INSERT INTO stripe_meter_reports (
    organization_id
  , cycle_start
  , seq
  , delta_tokens
  , delivery_state
) VALUES (
    @organization_id
  , @cycle_start
  , @seq
  , @delta_tokens
  , 'confirmed'
)
RETURNING *;

-- name: ListTUMCarryAllocationsFixture :many
SELECT *
FROM stripe_invoice_allocations
WHERE organization_id = @organization_id
  AND source_kind = 'tum_cycle'
ORDER BY source_period_start, seq;

-- name: GetPaygInvoiceIdentity :one
SELECT
    billing_metadata.stripe_customer_id
  , billing_metadata.stripe_subscription_id
  , billing_metadata.stripe_billing_cycle_anchor
  , billing_metadata.stripe_checkout_session_id
  , organization_metadata.gram_account_type
FROM billing_metadata
JOIN organization_metadata
  ON organization_metadata.id = billing_metadata.organization_id
WHERE billing_metadata.organization_id = @organization_id;

-- name: SetStripeCheckoutSessionFixture :exec
-- Test-only fixture for invoice/checkout webhook ordering.
UPDATE billing_metadata
SET stripe_checkout_session_id = @stripe_checkout_session_id
WHERE organization_id = @organization_id;

-- name: UpsertStripeInvoice :one
INSERT INTO stripe_invoices (
    stripe_invoice_id
  , organization_id
  , stripe_customer_id
  , stripe_subscription_id
  , service_period_start
  , service_period_end
  , invoice_state
  , finalized_at
) VALUES (
    @stripe_invoice_id
  , @organization_id
  , @stripe_customer_id
  , @stripe_subscription_id
  , @service_period_start
  , @service_period_end
  , @invoice_state
  , @finalized_at
)
ON CONFLICT (stripe_invoice_id) DO UPDATE
SET
    invoice_state = EXCLUDED.invoice_state
  , finalized_at = EXCLUDED.finalized_at
  , updated_at = clock_timestamp()
WHERE stripe_invoices.organization_id = EXCLUDED.organization_id
  AND stripe_invoices.stripe_customer_id = EXCLUDED.stripe_customer_id
  AND stripe_invoices.stripe_subscription_id = EXCLUDED.stripe_subscription_id
  AND stripe_invoices.service_period_start = EXCLUDED.service_period_start
  AND stripe_invoices.service_period_end = EXCLUDED.service_period_end
RETURNING stripe_invoice_id;

-- name: ListStripeInvoicesFixture :many
SELECT *
FROM stripe_invoices
WHERE organization_id = @organization_id
ORDER BY service_period_start, stripe_invoice_id;
