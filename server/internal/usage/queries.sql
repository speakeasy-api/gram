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
  , tunneled_mcp_server_limit = EXCLUDED.tunneled_mcp_server_limit
  , updated_at = clock_timestamp()
RETURNING *;

-- name: ListProjectIDsByOrganization :many
SELECT id
FROM projects
WHERE organization_id = @organization_id
  AND deleted IS FALSE;
