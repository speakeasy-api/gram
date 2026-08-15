-- name: GetPlatformUsageMetrics :many
-- Get comprehensive platform usage metrics per organization
WITH latest_deployments AS (
  SELECT DISTINCT ON (project_id) project_id, d.id as deployment_id
  FROM deployments d
  JOIN deployment_statuses ds ON d.id = ds.deployment_id
  WHERE ds.status = 'completed'
  ORDER BY project_id, d.created_at DESC
),
toolset_metrics AS (
  SELECT 
    p.organization_id,
    COUNT(CASE WHEN t.mcp_is_public = true AND t.mcp_slug IS NOT NULL THEN 1 END) as public_mcp_servers,
    COUNT(CASE WHEN t.mcp_is_public = false AND t.mcp_slug IS NOT NULL THEN 1 END) as private_mcp_servers,
    COUNT(CASE WHEN t.mcp_enabled = true THEN 1 END) as total_enabled_servers,
    COUNT(t.id) as total_toolsets
  FROM projects p
  LEFT JOIN toolsets t ON p.id = t.project_id AND t.deleted = false
  GROUP BY p.organization_id
),
tool_metrics AS (
  SELECT 
    p.organization_id,
    COUNT(DISTINCT htd.id) as total_tools
  FROM projects p
  LEFT JOIN latest_deployments ld ON p.id = ld.project_id
  LEFT JOIN http_tool_definitions htd ON ld.deployment_id = htd.deployment_id AND htd.deleted = false
  GROUP BY p.organization_id
)
SELECT 
  COALESCE(tm.organization_id, tlm.organization_id) as organization_id,
  COALESCE(tm.public_mcp_servers, 0) as public_mcp_servers,
  COALESCE(tm.private_mcp_servers, 0) as private_mcp_servers,
  COALESCE(tm.total_enabled_servers, 0) as total_enabled_servers,
  COALESCE(tm.total_toolsets, 0) as total_toolsets,
  COALESCE(tlm.total_tools, 0) as total_tools
FROM toolset_metrics tm
FULL OUTER JOIN tool_metrics tlm ON tm.organization_id = tlm.organization_id;

-- name: GetAllOrganizationsWithToolsets :many
SELECT
    organization_metadata.id,
    organization_metadata.name,
    organization_metadata.slug,
    gram_account_type
FROM organization_metadata
JOIN toolsets ON organization_metadata.id = toolsets.organization_id
WHERE toolsets.deleted = false
GROUP BY organization_metadata.id
HAVING COUNT(toolsets.id) > 0;

-- name: GetOpenRouterCreditsMonitoringTargets :many
-- Targets for periodic OpenRouter credit usage polling. Filters out disabled
-- orgs and disabled/deleted keys, and restricts to the caller-supplied
-- account-type allowlist so coverage can expand (e.g. add 'pro') without a
-- code change. monthly_credits is the canonical limit last written by
-- RefreshAPIKeyLimit and reflects any per-org overrides applied via the
-- OpenrouterKeyRefreshWorkflow. The key material is included so the caller
-- can issue the upstream usage HTTP call in a single round-trip — keep it
-- inside the activity boundary and never return it to the workflow. The
-- encrypted column is preferred and the plaintext column is the legacy
-- fallback for rows minted before encrypted storage.
SELECT
    om.id AS organization_id,
    om.slug AS organization_slug,
    om.gram_account_type,
    k.key_type,
    k.monthly_credits,
    k.key AS api_key,
    k.key_encrypted AS api_key_encrypted
FROM organization_metadata om
JOIN openrouter_api_keys k ON k.organization_id = om.id
WHERE om.disabled_at IS NULL
  AND k.disabled = FALSE
  AND k.deleted = FALSE
  AND om.gram_account_type = ANY(@account_types::text[])
ORDER BY om.slug;

-- name: ListOpenRouterDailySpendTargets :many
-- Every live platform-managed key is a billing input, including disabled keys:
-- disabling a key stops future usage but does not erase spend already reported
-- by OpenRouter. The key hash identifies the management-API analytics filter;
-- plaintext key material never crosses this activity boundary.
SELECT
    organization_id,
    key_type,
    key_hash,
    created_at
FROM openrouter_api_keys
WHERE deleted = FALSE
ORDER BY organization_id, key_type;

-- name: UpsertOpenRouterDailySpend :exec
-- Restatements replace a day's value, while an identical replay leaves
-- updated_at unchanged so operators can distinguish a real correction from a
-- routine overlapping pull.
INSERT INTO openrouter_spend_daily (
    organization_id,
    key_type,
    day,
    spend_usd
) VALUES (
    @target_organization_id,
    @target_key_type,
    @target_day,
    @target_spend_usd
)
ON CONFLICT (organization_id, key_type, day) DO UPDATE
SET
    spend_usd = EXCLUDED.spend_usd,
    updated_at = clock_timestamp()
WHERE openrouter_spend_daily.spend_usd IS DISTINCT FROM @target_spend_usd;

-- name: GetOpenRouterDailySpendRecoveryStartDay :one
-- Expand collection to the oldest unresolved invoice day. This heals outages
-- beyond the normal overlap instead of permanently detecting the same gap.
SELECT MIN(generated.source_timestamp)::date AS recovery_start_day
FROM stripe_invoices invoice
CROSS JOIN LATERAL generate_series(
  invoice.service_period_start,
  invoice.service_period_end - interval '1 day',
  interval '1 day'
) AS generated(source_timestamp)
WHERE invoice.organization_id = @target_organization_id
  AND generated.source_timestamp::date >= @target_earliest_day::date
  AND generated.source_timestamp::date < @target_end_day::date
  AND (
    (
      invoice.service_period_end + interval '48 hours' <= @target_end_day::date
      AND NOT EXISTS (
        SELECT 1
        FROM stripe_invoice_allocations allocation
        WHERE allocation.organization_id = invoice.organization_id
          AND allocation.source_kind = 'openrouter_daily_spend'
          AND allocation.source_key = generated.source_timestamp::date::text || ':chat'
          AND allocation.seq = 1
      )
    )
    OR (
      invoice.service_period_end + interval '72 hours' <= @target_end_day::date
      AND NOT EXISTS (
        SELECT 1
        FROM stripe_invoice_allocations allocation
        WHERE allocation.organization_id = invoice.organization_id
          AND allocation.source_kind = 'openrouter_daily_spend'
          AND allocation.source_key = generated.source_timestamp::date::text || ':chat'
          AND allocation.seq = 2
      )
    )
  );

-- name: CountOpenRouterInvoiceSpendGaps :one
SELECT COUNT(*)::bigint AS missing_count
FROM stripe_invoices invoice
CROSS JOIN LATERAL generate_series(
  invoice.service_period_start,
  invoice.service_period_end - interval '1 day',
  interval '1 day'
) AS generated(source_timestamp)
LEFT JOIN openrouter_spend_daily spend
  ON spend.organization_id = invoice.organization_id
 AND spend.key_type = @target_key_type
 AND spend.day = generated.source_timestamp::date
WHERE invoice.organization_id = @target_organization_id
  AND generated.source_timestamp::date >= @target_earliest_day::date
  AND generated.source_timestamp::date < @target_end_day::date
  AND spend.id IS NULL
  AND (
    (
      invoice.service_period_end + interval '48 hours' <= @target_end_day::date
      AND NOT EXISTS (
        SELECT 1
        FROM stripe_invoice_allocations allocation
        WHERE allocation.organization_id = invoice.organization_id
          AND allocation.source_kind = 'openrouter_daily_spend'
          AND allocation.source_key = generated.source_timestamp::date::text || ':chat'
          AND allocation.seq = 1
      )
    )
    OR (
      invoice.service_period_end + interval '72 hours' <= @target_end_day::date
      AND NOT EXISTS (
        SELECT 1
        FROM stripe_invoice_allocations allocation
        WHERE allocation.organization_id = invoice.organization_id
          AND allocation.source_kind = 'openrouter_daily_spend'
          AND allocation.source_key = generated.source_timestamp::date::text || ':chat'
          AND allocation.seq = 2
      )
    )
  );

-- name: ListOpenRouterDailySpend :many
SELECT
    organization_id,
    key_type,
    day,
    spend_usd,
    created_at,
    updated_at
FROM openrouter_spend_daily
WHERE organization_id = @organization_id
  AND key_type = @key_type
ORDER BY day;

-- name: ListStripeInvoiceBillingOrganizations :many
-- Organizations remain candidates for as long as a required immutable
-- snapshot, a delivery, or a destination assignment is missing. There is no
-- age cutoff: an outage must delay billing rather than erase it.
SELECT DISTINCT invoice.organization_id::text AS organization_id
FROM stripe_invoices invoice
WHERE invoice.organization_id IS NOT NULL
  AND (
    EXISTS (
      SELECT 1
      FROM generate_series(
        invoice.service_period_start,
        invoice.service_period_end - interval '1 day',
        interval '1 day'
      ) AS source_day
      WHERE invoice.service_period_end + interval '48 hours' <= @now
        AND NOT EXISTS (
          SELECT 1
          FROM stripe_invoice_allocations allocation
          WHERE allocation.organization_id = invoice.organization_id
            AND allocation.source_kind = 'openrouter_daily_spend'
            AND allocation.source_key = source_day::date::text || ':chat'
            AND allocation.seq = 1
        )
    )
    OR EXISTS (
      SELECT 1
      FROM generate_series(
        invoice.service_period_start,
        invoice.service_period_end - interval '1 day',
        interval '1 day'
      ) AS source_day
      WHERE invoice.service_period_end + interval '72 hours' <= @now
        AND NOT EXISTS (
          SELECT 1
          FROM stripe_invoice_allocations allocation
          WHERE allocation.organization_id = invoice.organization_id
            AND allocation.source_kind = 'openrouter_daily_spend'
            AND allocation.source_key = source_day::date::text || ':chat'
            AND allocation.seq = 2
        )
    )
    OR EXISTS (
      SELECT 1
      FROM stripe_invoice_allocations allocation
      WHERE allocation.organization_id = invoice.organization_id
        AND (
          allocation.delivery_state IN ('pending', 'ambiguous')
          OR (
            allocation.amount_usd > 0
            AND allocation.destination_invoice_id IS NULL
            AND allocation.original_invoice_id IS NOT NULL
          )
          OR (
            allocation.source_kind = 'tum_cycle'
            AND allocation.original_invoice_id IS NULL
          )
        )
    )
  )
ORDER BY organization_id;

-- name: ListStripeInvoicesForOpenRouterBilling :many
SELECT
    stripe_invoice_id
  , organization_id::text AS organization_id
  , stripe_customer_id
  , stripe_subscription_id
  , service_period_start
  , service_period_end
  , invoice_state
  , finalized_at
FROM stripe_invoices invoice
WHERE invoice.organization_id = @organization_id
  AND (
    EXISTS (
      SELECT 1
      FROM generate_series(
        invoice.service_period_start,
        invoice.service_period_end - interval '1 day',
        interval '1 day'
      ) AS source_day
      WHERE invoice.service_period_end + interval '48 hours' <= @now
        AND NOT EXISTS (
          SELECT 1
          FROM stripe_invoice_allocations allocation
          WHERE allocation.organization_id = invoice.organization_id
            AND allocation.source_kind = 'openrouter_daily_spend'
            AND allocation.source_key = source_day::date::text || ':chat'
            AND allocation.seq = 1
        )
    )
    OR EXISTS (
      SELECT 1
      FROM generate_series(
        invoice.service_period_start,
        invoice.service_period_end - interval '1 day',
        interval '1 day'
      ) AS source_day
      WHERE invoice.service_period_end + interval '72 hours' <= @now
        AND NOT EXISTS (
          SELECT 1
          FROM stripe_invoice_allocations allocation
          WHERE allocation.organization_id = invoice.organization_id
            AND allocation.source_kind = 'openrouter_daily_spend'
            AND allocation.source_key = source_day::date::text || ':chat'
            AND allocation.seq = 2
        )
    )
    OR EXISTS (
      SELECT 1
      FROM stripe_invoice_allocations allocation
      WHERE allocation.organization_id = invoice.organization_id
        AND (
          allocation.original_invoice_id = invoice.stripe_invoice_id
          OR allocation.destination_invoice_id = invoice.stripe_invoice_id
          OR (
            invoice.invoice_state = 'draft'
            AND allocation.amount_usd > 0
            AND allocation.destination_invoice_id IS NULL
            AND allocation.original_invoice_id IS NOT NULL
          )
          OR (
            allocation.source_kind = 'tum_cycle'
            AND allocation.original_invoice_id IS NULL
            AND allocation.source_period_start = invoice.service_period_start
            AND allocation.source_period_end = invoice.service_period_end
          )
        )
    )
  )
ORDER BY service_period_start;

-- name: UpdateStripeInvoiceState :execrows
UPDATE stripe_invoices
SET invoice_state = @invoice_state,
    finalized_at = @finalized_at,
    updated_at = clock_timestamp()
WHERE organization_id = @organization_id
  AND stripe_invoice_id = @stripe_invoice_id
  AND stripe_customer_id = @stripe_customer_id
  AND stripe_subscription_id = @stripe_subscription_id
  AND service_period_start = @service_period_start
  AND service_period_end = @service_period_end;

-- name: ListOpenRouterInvoiceSourceDays :many
SELECT
    generated.source_timestamp::date AS source_day
  , COALESCE(spend.spend_usd, 0::numeric) AS spend_usd
FROM stripe_invoices invoice
CROSS JOIN LATERAL generate_series(
  invoice.service_period_start,
  invoice.service_period_end - interval '1 day',
  interval '1 day'
) AS generated(source_timestamp)
LEFT JOIN openrouter_spend_daily spend
  ON spend.organization_id = invoice.organization_id
 AND spend.key_type = 'chat'
 AND spend.day = generated.source_timestamp::date
WHERE invoice.organization_id = @organization_id
  AND invoice.stripe_invoice_id = @stripe_invoice_id
  AND invoice.service_period_end + interval '48 hours' <= @now
ORDER BY source_day;

-- name: ListOpenRouterInvoiceBaselines :many
SELECT
    allocation.source_key
  , allocation.source_day
  , allocation.source_snapshot_usd
  , (CASE
      WHEN chat_key.created_at IS NULL
        OR allocation.source_day < (chat_key.created_at AT TIME ZONE 'UTC')::date
        THEN 0::numeric
      ELSE spend.spend_usd
    END)::numeric(14, 6) AS final_spend_usd
  , carry.amount_usd AS existing_carry_amount_usd
  , allocation.original_invoice_id
FROM stripe_invoice_allocations allocation
JOIN stripe_invoices invoice
  ON invoice.organization_id = allocation.organization_id
 AND invoice.stripe_invoice_id = allocation.original_invoice_id
LEFT JOIN openrouter_api_keys chat_key
  ON chat_key.organization_id = allocation.organization_id
 AND chat_key.key_type = 'chat'
LEFT JOIN openrouter_spend_daily spend
  ON spend.organization_id = allocation.organization_id
 AND spend.key_type = 'chat'
 AND spend.day = allocation.source_day
LEFT JOIN stripe_invoice_allocations carry
  ON carry.organization_id = allocation.organization_id
 AND carry.source_kind = allocation.source_kind
 AND carry.source_key = allocation.source_key
 AND carry.seq = 2
WHERE allocation.organization_id = @organization_id
  AND invoice.stripe_invoice_id = @stripe_invoice_id
  AND invoice.service_period_end + interval '72 hours' <= @now
  AND allocation.source_kind = 'openrouter_daily_spend'
  AND allocation.seq = 1
ORDER BY allocation.source_day;

-- name: CreateOpenRouterInvoiceAllocation :execrows
INSERT INTO stripe_invoice_allocations (
    organization_id
  , source_kind
  , source_key
  , seq
  , source_day
  , source_snapshot_usd
  , amount_usd
  , original_invoice_id
  , destination_invoice_id
  , idempotency_key
  , delivery_state
  , confirmed_at
) VALUES (
    @organization_id
  , 'openrouter_daily_spend'
  , @source_key
  , @seq
  , @source_day
  , @source_snapshot_usd
  , @amount_usd
  , @original_invoice_id
  , @destination_invoice_id
  , @idempotency_key
  , @delivery_state
  , @confirmed_at
)
ON CONFLICT (organization_id, source_kind, source_key, seq) DO NOTHING;

-- name: AttachTUMCarryToOriginalInvoice :execrows
UPDATE stripe_invoice_allocations allocation
SET original_invoice_id = invoice.stripe_invoice_id,
    destination_invoice_id = CASE WHEN allocation.amount_usd < 0 THEN invoice.stripe_invoice_id END,
    updated_at = clock_timestamp()
FROM stripe_invoices invoice
WHERE allocation.organization_id = @organization_id
  AND allocation.source_kind = 'tum_cycle'
  AND allocation.original_invoice_id IS NULL
  AND invoice.organization_id = allocation.organization_id
  AND invoice.service_period_start = allocation.source_period_start
  AND invoice.service_period_end = allocation.source_period_end;

-- name: AssignPositiveCarryToStripeInvoice :execrows
-- Assign every positive carry to the earliest eligible draft. The NULL guard
-- is the compare-and-swap fence for concurrent settlement runs.
WITH assignments AS (
  SELECT
      allocation.id
    , (
        SELECT candidate.stripe_invoice_id
        FROM stripe_invoices original
        JOIN stripe_invoices candidate
          ON candidate.organization_id = original.organization_id
         AND candidate.invoice_state = 'draft'
         AND original.service_period_end <= candidate.service_period_start
        WHERE original.organization_id = allocation.organization_id
          AND original.stripe_invoice_id = allocation.original_invoice_id
        ORDER BY candidate.service_period_start, candidate.stripe_invoice_id
        LIMIT 1
      ) AS destination_invoice_id
  FROM stripe_invoice_allocations allocation
  WHERE allocation.organization_id = @organization_id
    AND allocation.amount_usd > 0
    AND allocation.destination_invoice_id IS NULL
    AND allocation.original_invoice_id IS NOT NULL
)
UPDATE stripe_invoice_allocations allocation
SET destination_invoice_id = assignments.destination_invoice_id,
    updated_at = clock_timestamp()
FROM assignments
WHERE allocation.organization_id = @organization_id
  AND allocation.id = assignments.id
  AND assignments.destination_invoice_id IS NOT NULL
  AND allocation.amount_usd > 0
  AND allocation.destination_invoice_id IS NULL
  AND allocation.original_invoice_id IS NOT NULL;

-- name: ClaimNextStripeInvoiceAllocation :one
WITH candidate AS (
  SELECT
      allocation.id
    , allocation.first_attempted_at AS previous_first_attempted_at
    , allocation.delivery_state AS previous_delivery_state
  FROM stripe_invoice_allocations allocation
  JOIN stripe_invoices destination
    ON destination.stripe_invoice_id = allocation.destination_invoice_id
   AND destination.organization_id = allocation.organization_id
  WHERE allocation.organization_id = @organization_id
    AND allocation.delivery_state IN ('pending', 'ambiguous')
    AND allocation.amount_usd <> 0
    AND (
      allocation.last_attempted_at IS NULL
      OR allocation.last_attempted_at <= @lease_before
    )
  ORDER BY allocation.created_at, allocation.source_kind, allocation.source_key, allocation.seq
  LIMIT 1
  FOR UPDATE OF allocation SKIP LOCKED
), claimed AS (
  UPDATE stripe_invoice_allocations allocation
  SET first_attempted_at = COALESCE(allocation.first_attempted_at, @attempted_at),
      last_attempted_at = @attempted_at,
      delivery_state = 'pending',
      updated_at = clock_timestamp()
  FROM candidate
  WHERE allocation.organization_id = @organization_id
    AND allocation.id = candidate.id
  RETURNING
    allocation.id
  , allocation.organization_id::text AS organization_id
  , allocation.source_kind
  , allocation.source_key
  , allocation.seq
  , allocation.source_day
  , allocation.source_period_start
  , allocation.source_period_end
  , allocation.amount_usd
  , allocation.original_invoice_id
  , allocation.destination_invoice_id
  , allocation.idempotency_key
  , allocation.delivery_state
  , candidate.previous_first_attempted_at
  , candidate.previous_delivery_state
)
SELECT
    claimed.*
  , destination.stripe_customer_id
  , destination.stripe_subscription_id
  , destination.service_period_start AS destination_period_start
  , destination.service_period_end AS destination_period_end
  , destination.invoice_state AS destination_invoice_state
FROM claimed
JOIN stripe_invoices destination
  ON destination.stripe_invoice_id = claimed.destination_invoice_id
 AND destination.organization_id = claimed.organization_id;

-- name: MarkStripeInvoiceAllocationAmbiguous :execrows
UPDATE stripe_invoice_allocations
SET first_attempted_at = COALESCE(first_attempted_at, @attempted_at),
    last_attempted_at = @attempted_at,
    ambiguous_at = COALESCE(ambiguous_at, @attempted_at),
    delivery_state = 'ambiguous',
    updated_at = clock_timestamp()
WHERE organization_id = @organization_id
  AND id = @id
  AND delivery_state = 'pending'
  AND last_attempted_at = @attempted_at;

-- name: ReconcileAndRotateStripeInvoiceAllocation :one
UPDATE stripe_invoice_allocations
SET reconciled_at = @reconciled_at,
    idempotency_key = regexp_replace(idempotency_key, ':retry:[0-9]+$', '')
      || ':retry:' || extract(epoch FROM @reconciled_at::timestamptz)::bigint::text,
    first_attempted_at = @reconciled_at,
    ambiguous_at = NULL,
    updated_at = clock_timestamp()
WHERE organization_id = @organization_id
  AND id = @id
  AND delivery_state = 'pending'
  AND last_attempted_at = @attempted_at
RETURNING idempotency_key;

-- name: UnassignPositiveStripeInvoiceAllocation :execrows
UPDATE stripe_invoice_allocations
SET destination_invoice_id = NULL,
    updated_at = clock_timestamp()
WHERE organization_id = @organization_id
  AND id = @id
  AND amount_usd > 0
  AND delivery_state = 'pending'
  AND last_attempted_at = @attempted_at;

-- name: ConfirmStripeInvoiceItemAllocation :execrows
UPDATE stripe_invoice_allocations
SET stripe_invoice_item_id = @stripe_invoice_item_id,
    delivery_state = 'confirmed',
    confirmed_at = COALESCE(confirmed_at, @confirmed_at),
    reconciled_at = CASE WHEN @reconciled::boolean THEN @confirmed_at ELSE reconciled_at END,
    updated_at = clock_timestamp()
WHERE organization_id = @organization_id
  AND id = @id
  AND delivery_state = 'pending'
  AND last_attempted_at = @attempted_at;

-- name: ConfirmStripeCreditNoteAllocation :execrows
UPDATE stripe_invoice_allocations
SET stripe_credit_note_id = @stripe_credit_note_id,
    delivery_state = 'confirmed',
    confirmed_at = COALESCE(confirmed_at, @confirmed_at),
    reconciled_at = CASE WHEN @reconciled::boolean THEN @confirmed_at ELSE reconciled_at END,
    updated_at = clock_timestamp()
WHERE organization_id = @organization_id
  AND id = @id
  AND delivery_state = 'pending'
  AND last_attempted_at = @attempted_at;

-- name: SetOpenRouterAPIKeyCreatedAtFixture :exec
UPDATE openrouter_api_keys
SET created_at = @created_at
WHERE organization_id = @organization_id
  AND key_type = @key_type;

-- name: CreateStripeInvoiceFixture :exec
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
);

-- name: CreateTUMInvoiceAllocationFixture :exec
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
) VALUES (
    @organization_id
  , 'tum_cycle'
  , @source_key
  , 1
  , @source_period_start
  , @source_period_end
  , @source_snapshot_usd
  , 1
  , 0.000000350000
  , @amount_usd
  , @idempotency_key
  , 'pending'
);

-- name: ListStripeInvoiceAllocationsFixture :many
SELECT
    seq
  , source_kind
  , source_key
  , source_snapshot_usd
  , amount_usd
  , original_invoice_id
  , destination_invoice_id
  , idempotency_key
  , delivery_state
  , stripe_invoice_item_id
  , stripe_credit_note_id
  , first_attempted_at
  , last_attempted_at
  , reconciled_at
FROM stripe_invoice_allocations
WHERE organization_id = @organization_id
ORDER BY source_key, seq;

-- name: GetOpenRouterCreditsAlertRecipients :many
-- Resolve the billing alert recipient for each supplied organization that
-- should receive an OpenRouter credit threshold warning. An org qualifies only
-- if it is not disabled and has a billing alert email configured (the address
-- set on the billing page). chat_byok reports whether the org has an enabled,
-- non-deleted customer-supplied model provider key outside the internal-only
-- slots (@internal_only_slots, the platform-initiated judge slots that never
-- carry chat completions); such a key means the platform chat key is not what
-- pays for the org's completions, so the caller suppresses chat-key warnings —
-- deliberately org-wide, matching the ticket-level "no alerts for BYOK orgs"
-- decision. The flag is returned rather than filtered on because it only
-- applies to some key types: usage on the internal key is platform-billed
-- regardless of any customer keys.
SELECT
    om.id AS organization_id,
    om.name AS organization_name,
    bm.alert_email,
    EXISTS (
        SELECT 1
        FROM model_provider_keys mpk
        WHERE mpk.organization_id = om.id
          AND mpk.enabled = TRUE
          AND mpk.deleted = FALSE
          AND mpk.slot <> ALL(@internal_only_slots::text[])
    )::boolean AS chat_byok
FROM organization_metadata om
JOIN billing_metadata bm ON bm.organization_id = om.id
WHERE om.id = ANY(@organization_ids::text[])
  AND om.disabled_at IS NULL
  AND bm.alert_email IS NOT NULL;

-- name: ListWeeklyUsageSummaryTargets :many
-- Organizations that receive the weekly tokens-under-management usage
-- summary email: not disabled, with a billing alert email configured (the
-- address set on the billing page). The anchor day determines the billing
-- cycle window the summary reports on; the slug builds the billing page
-- link.
SELECT
    om.id AS organization_id,
    om.name AS organization_name,
    om.slug AS organization_slug,
    bm.alert_email,
    bm.billing_cycle_anchor_day
FROM organization_metadata om
JOIN billing_metadata bm ON bm.organization_id = om.id
WHERE om.disabled_at IS NULL
  AND bm.alert_email IS NOT NULL
ORDER BY om.slug;

-- name: GetUserEmailsByOrgIDs :many
-- Get user emails for organization IDs by looking up the latest deployment for each org
SELECT DISTINCT
    d.organization_id,
    u.email
FROM deployments d
JOIN users u ON d.user_id = u.id
WHERE d.organization_id = ANY($1::text[])
  AND d.id IN (
    SELECT DISTINCT ON (organization_id) id
    FROM deployments
    WHERE organization_id = ANY($1::text[])
    ORDER BY organization_id, created_at DESC
  );

-- name: ListUnlinkedClaudeUserMessagesForCorrelation :many
-- Fetch a bounded prefix of the unlinked backlog. The caller requests one extra
-- row to detect whether another drain pass is needed.
SELECT id, seq, content, created_at
FROM chat_messages
WHERE chat_id = @chat_id
  AND project_id = @project_id
  AND role = 'user'
  AND content != ''
  AND (message_id IS NULL OR message_id = '')
  AND seq > @after_message_seq
ORDER BY seq ASC, created_at ASC
LIMIT @limit_count;

-- name: BackfillClaudeUserMessagePromptID :exec
UPDATE chat_messages
SET message_id = @prompt_id
WHERE id = @message_id
  AND chat_id = @chat_id
  AND project_id = @project_id
  AND role = 'user'
  AND (message_id IS NULL OR message_id = '');

-- name: FetchPendingOutboxIDs :many
-- Fetch the next batch of outbox row IDs (across all organizations) that the
-- Svix relay has not finished processing. A row is "pending" when no relay
-- tracking row exists OR a tracking row exists with processed_at IS NULL and
-- not dead-lettered. Returns only IDs to keep the activity payload small —
-- workflows pass IDs to RelayBatch which re-queries the full rows.
SELECT o.id, o.organization_id, om.svix_app_id, om.webhooks_enabled
FROM outbox o
LEFT JOIN organization_metadata om ON o.organization_id = om.id
LEFT JOIN outbox_relays r ON r.outbox_id = o.id
WHERE r.outbox_id IS NULL OR (r.processed_at IS NULL AND r.dead_lettered IS FALSE AND (r.retry_after IS NULL OR r.retry_after <= clock_timestamp()))
ORDER BY o.id ASC
LIMIT @batch_size;

-- name: FetchOutboxRowsByIDs :many
-- Hydrate a set of outbox IDs back into full rows along with their current
-- relay attempt count. Intended to be called inside the relay activity after
-- the workflow has handed it a batch of IDs.
SELECT
    o.id,
    o.public_id,
    o.organization_id,
    o.event_type,
    o.payload,
    COALESCE(r.attempts, 0)::int AS attempts
FROM outbox o
LEFT JOIN outbox_relays r ON r.outbox_id = o.id
WHERE o.id = ANY(@ids::bigint[])
ORDER BY o.id ASC;

-- name: MarkOutboxRelayProcessed :exec
-- Marks a relay as successfully delivered to Svix.
INSERT INTO outbox_relays (outbox_id, processed_at, svix_message_id, attempts, last_error)
VALUES (@outbox_id, clock_timestamp(), @svix_message_id, 1, NULL)
ON CONFLICT (outbox_id) DO UPDATE SET
    processed_at = clock_timestamp(),
    svix_message_id = EXCLUDED.svix_message_id,
    attempts = outbox_relays.attempts + 1,
    last_error = NULL,
    updated_at = clock_timestamp();

-- name: MarkOutboxRelayFailed :exec
-- Records a failed delivery attempt; the row remains pending for retry.
INSERT INTO outbox_relays (outbox_id, attempts, last_error, retry_after)
VALUES (@outbox_id, 1, @last_error, @retry_after)
ON CONFLICT (outbox_id) DO UPDATE SET
    attempts = outbox_relays.attempts + 1,
    last_error = EXCLUDED.last_error,
    retry_after = EXCLUDED.retry_after,
    updated_at = clock_timestamp();

-- name: GCProcessedOutboxRows :execrows
-- Hard-deletes terminal outbox rows older than @cutoff. Terminal means the
-- relay row is processed, noop, or dead-lettered. The cascade FK removes the
-- outbox_relays row automatically. Batched via LIMIT to bound lock time.
DELETE FROM outbox
WHERE id IN (
  SELECT o.id
  FROM outbox o
  JOIN outbox_relays r ON r.outbox_id = o.id
  WHERE o.created_at < @cutoff
    AND (r.processed_at IS NOT NULL OR r.noop = TRUE OR r.dead_lettered = TRUE)
  ORDER BY o.id ASC
  LIMIT @batch_size
);

-- name: MarkOutboxRelayDeadLettered :exec
-- Permanently parks a row after exceeding the retry budget. The pending
-- partial index excludes dead_lettered rows so they will not be re-fetched.
INSERT INTO outbox_relays (outbox_id, attempts, last_error, dead_lettered)
VALUES (@outbox_id, 1, @last_error, TRUE)
ON CONFLICT (outbox_id) DO UPDATE SET
    attempts = outbox_relays.attempts + 1,
    last_error = EXCLUDED.last_error,
    dead_lettered = TRUE,
    updated_at = clock_timestamp();

-- name: ClaimPublishOutboxBatch :many
-- Leases a batch of publishable rows to this drainer. The lease plus SKIP
-- LOCKED is what makes concurrent drains safe: two workers claim disjoint sets
-- rather than racing to publish the same row. attempts is incremented here
-- rather than on failure so it counts deliveries attempted, which is what the
-- dead-letter threshold acts on. The statement commits on its own — the caller
-- must not hold a transaction open across the Pub/Sub round trip, or a stalled
-- publish would pin an XID and block vacuum database-wide.
--
-- locked_until doubles as the claim's fencing token, which is why the
-- settlement statements match on it. A row can only be re-claimed once its
-- lease has elapsed, so each claim sets a value strictly greater than the last,
-- and a drain that overran its lease finds no row to settle instead of
-- overwriting the claim that replaced it.
--
-- lease_token identifies this claim, and the settlement statements match on it
-- so a drain that overran its lease finds no row to settle instead of
-- overwriting the claim that replaced it. The caller mints it: gen_random_uuid()
-- is volatile and would evaluate per row, giving every row in one batch a
-- different token and leaving settlement no single value to match.
UPDATE publish_outbox SET
    locked_until = clock_timestamp() + @lease::interval,
    lease_token = @lease_token::uuid,
    attempts = attempts + 1,
    updated_at = clock_timestamp()
WHERE id IN (
  SELECT o.id
  FROM publish_outbox o
  WHERE (o.retry_after IS NULL OR o.retry_after <= clock_timestamp())
    AND (o.locked_until IS NULL OR o.locked_until <= clock_timestamp())
  ORDER BY o.id ASC
  LIMIT @batch_size
  FOR UPDATE SKIP LOCKED
)
RETURNING id, public_id, organization_id, topic, message, attributes, attempts, created_at;

-- name: DeletePublishedOutboxRows :execrows
-- Removes rows whose publish was acknowledged by Pub/Sub. Deleting rather than
-- marking is what keeps the table near-empty and its updates cheap.
--
-- Matched on the lease as well as the id, so a drain that outlived its own
-- lease cannot settle a row another worker has since claimed. See
-- ClaimPublishOutboxBatch for why locked_until identifies the claim.
DELETE FROM publish_outbox
WHERE id = ANY(@ids::bigint[])
  AND lease_token = @lease_token::uuid;

-- name: MarkPublishOutboxFailed :exec
-- Records a transient publish failure and releases the lease so the row is
-- eligible again once retry_after elapses.
--
-- Both the delay and the error arrive per row, expanded in lockstep with the
-- ids. A claim is ordered by id, so one batch mixes rows on their first attempt
-- with rows deep into their back-off; a single timestamp for the batch would
-- hand every row the shortest delay among them, and back-off would never
-- escalate for as long as new rows kept arriving. The error names the topic the
-- row could not reach, so one shared string labels most of the batch with a
-- topic they have nothing to do with — and last_error is what anyone looking
-- into a stuck row reads.
UPDATE publish_outbox SET
    last_error = settlement.last_error,
    retry_after = settlement.retry_after,
    locked_until = NULL,
    lease_token = NULL,
    updated_at = clock_timestamp()
FROM (
  SELECT unnest(@ids::bigint[]) AS id,
         unnest(@errors::text[]) AS last_error,
         unnest(@retry_afters::timestamptz[]) AS retry_after
) AS settlement
WHERE publish_outbox.id = settlement.id
  AND publish_outbox.lease_token = @lease_token::uuid;

-- name: DeadLetterPublishOutboxRows :execrows
-- Moves rows that can never publish out of the queue in one statement, so a
-- crash cannot leave a row both dead-lettered and still pending.
--
-- Each row carries the error that stopped it, expanded in lockstep with the
-- ids. One batch can hold an unregistered topic next to an oversized payload,
-- and this table is the permanent forensic record: an operator triaging a dead
-- letter has nothing else to read, so a row stamped with its neighbour's
-- failure sends them after a problem that row does not have.
WITH failures AS (
  SELECT unnest(@ids::bigint[]) AS id,
         unnest(@errors::text[]) AS last_error
), moved AS (
  DELETE FROM publish_outbox
  WHERE id = ANY(@ids::bigint[])
    AND lease_token = @lease_token::uuid
  RETURNING id, public_id, organization_id, topic, message, attributes, attempts,
            created_at AS row_enqueued_at
)
INSERT INTO publish_outbox_dead_letters (
  public_id, organization_id, topic, message, attributes, attempts, last_error, enqueued_at
)
SELECT moved.public_id, moved.organization_id, moved.topic, moved.message,
       moved.attributes, moved.attempts, failures.last_error, moved.row_enqueued_at
FROM moved
JOIN failures ON failures.id = moved.id;

-- name: GCPublishOutboxDeadLetters :execrows
-- Bounds the dead letter table. Batched via LIMIT to keep lock time short.
DELETE FROM publish_outbox_dead_letters
WHERE id IN (
  SELECT d.id
  FROM publish_outbox_dead_letters d
  WHERE d.created_at < @cutoff
  ORDER BY d.id ASC
  LIMIT @batch_size
);

-- name: CountPendingPublishOutboxRows :one
-- Backs the queue depth gauge. Cheap only because the table is near-empty in
-- steady state; a growing count is itself the signal worth alerting on.
SELECT COUNT(*) FROM publish_outbox;

-- name: ReleasePublishOutboxRows :exec
-- Drops the lease on rows claimed but not acted upon, so the next drain sees
-- them immediately instead of waiting out the lease. attempts is decremented
-- back because the claim incremented it for a delivery that never happened.
UPDATE publish_outbox SET
    locked_until = NULL,
    lease_token = NULL,
    attempts = GREATEST(attempts - 1, 0),
    updated_at = clock_timestamp()
WHERE id = ANY(@ids::bigint[])
  AND lease_token = @lease_token::uuid;

-- name: ListIdentityMapEntries :many
-- Source of the ClickHouse identity_map fold table. Internal sync sweep that
-- deliberately spans all organizations: the org boundary is carried in the
-- output rows (organization_id keys the map), not the predicate; not reachable
-- from user-facing handlers.
--
-- Encodes the employee identity fold rules (the SQL twin of telemetry's
-- resolveEmployeeIdentity, which DNO-857 retires): a directory email maps to
-- its user only when exactly one connected, non-deleted user claims it
-- case-insensitively; a linked-account email maps to its owner only when the
-- email has no directory row in the org and exactly one connected owner with
-- an unambiguous directory email claims it. Ambiguous emails are omitted so
-- readers fall back to literal matching rather than guessing.
--
-- The owner-ambiguity count deliberately includes account links whose owner is
-- since deleted or disconnected (matching the Go resolver): a second historical
-- claimant has telemetry rows under the shared email, and folding it to the
-- surviving owner would move the departed claimant's usage onto them.
WITH directory AS (
    SELECT
        our.organization_id,
        lower(btrim(u.email)) AS email_lower,
        min(u.id) AS user_id,
        count(*) AS claimants
    FROM users u
    JOIN organization_user_relationships our ON our.user_id = u.id
    WHERE u.deleted_at IS NULL
      AND our.deleted_at IS NULL
      AND btrim(u.email) != ''
    GROUP BY our.organization_id, lower(btrim(u.email))
), account_owners AS (
    SELECT ua.organization_id, lower(btrim(ua.email)) AS email_lower, ua.user_id
    FROM user_accounts ua
    WHERE ua.deleted_at IS NULL
      AND ua.user_id IS NOT NULL
      AND ua.email IS NOT NULL
      AND btrim(ua.email) != ''
    GROUP BY ua.organization_id, lower(btrim(ua.email)), ua.user_id
), unique_account_owner AS (
    SELECT organization_id, email_lower, min(user_id) AS user_id
    FROM account_owners
    GROUP BY organization_id, email_lower
    HAVING count(*) = 1
)
SELECT
    d.organization_id,
    d.email_lower,
    d.user_id::text AS canonical_user_id,
    d.email_lower AS canonical_email
FROM directory d
WHERE d.claimants = 1
UNION ALL
SELECT
    uao.organization_id,
    uao.email_lower,
    d.user_id::text AS canonical_user_id,
    d.email_lower AS canonical_email
FROM unique_account_owner uao
JOIN users u ON u.id = uao.user_id AND u.deleted_at IS NULL
JOIN directory d
    ON d.organization_id = uao.organization_id
    AND d.email_lower = lower(btrim(u.email))
    AND d.user_id = u.id
    AND d.claimants = 1
WHERE NOT EXISTS (
    SELECT 1 FROM directory dd
    WHERE dd.organization_id = uao.organization_id
      AND dd.email_lower = uao.email_lower
)
ORDER BY organization_id, email_lower;
