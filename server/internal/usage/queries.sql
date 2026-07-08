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

-- name: ListProjectIDsByOrganization :many
SELECT id
FROM projects
WHERE organization_id = @organization_id
  AND deleted IS FALSE;

-- name: UpsertBillingCycleUsage :exec
INSERT INTO billing_cycle_usage (
    organization_id
  , cycle_start
  , cycle_end
  , tum_tokens
  , finalized_at
) VALUES (
    @organization_id
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
WHERE organization_id = @organization_id
  AND finalized_at IS NOT NULL;

-- name: ListBillingCycleUsage :many
SELECT *
FROM billing_cycle_usage
WHERE organization_id = @organization_id
ORDER BY cycle_start;
