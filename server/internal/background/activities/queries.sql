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

-- name: FetchPendingOutboxIDs :many
-- Fetch the next batch of outbox row IDs for an organization that the Svix
-- relay has not finished processing. A row is "pending" when no relay
-- tracking row exists OR a tracking row exists with processed_at IS NULL and
-- not dead-lettered. Returns only IDs to keep the activity payload small —
-- workflows pass IDs to RelayBatch which re-queries the full rows.
SELECT o.id, o.organization_id, om.svix_app_id, om.webhooks_enabled
FROM outbox o
LEFT JOIN organization_metadata om ON o.organization_id = om.id
LEFT JOIN outbox_svix_relays r ON r.outbox_id = o.id
WHERE r.outbox_id IS NULL OR (r.processed_at IS NULL AND r.dead_lettered IS FALSE)
ORDER BY o.id ASC
LIMIT @batch_size;

-- name: GetOrganizationSvixAppID :one
-- Returns the organization's current svix_app_id (nullable).
SELECT svix_app_id, webhooks_enabled
FROM organization_metadata
WHERE id = @id;

-- name: FetchOutboxRowsByIDs :many
-- Hydrate a set of outbox IDs back into full rows along with their current
-- relay attempt count. Intended to be called inside the relay activity after
-- the workflow has handed it a batch of IDs.
SELECT
    o.id,
    o.organization_id,
    o.event_type,
    o.payload,
    COALESCE(r.attempts, 0)::int AS attempts
FROM outbox o
LEFT JOIN outbox_svix_relays r ON r.outbox_id = o.id
WHERE o.id = ANY(@ids::bigint[])
ORDER BY o.id ASC;

-- name: MarkOutboxRelayProcessed :exec
-- Marks a relay as successfully delivered to Svix.
INSERT INTO outbox_svix_relays (outbox_id, processed_at, svix_message_id, attempts, last_error)
VALUES (@outbox_id, clock_timestamp(), @svix_message_id, 1, NULL)
ON CONFLICT (outbox_id) DO UPDATE SET
    processed_at = clock_timestamp(),
    svix_message_id = EXCLUDED.svix_message_id,
    attempts = outbox_svix_relays.attempts + 1,
    last_error = NULL,
    updated_at = clock_timestamp();

-- name: MarkOutboxRelayFailed :exec
-- Records a failed delivery attempt; the row remains pending for retry.
INSERT INTO outbox_svix_relays (outbox_id, attempts, last_error)
VALUES ($1, 1, $2)
ON CONFLICT (outbox_id) DO UPDATE SET
    attempts = outbox_svix_relays.attempts + 1,
    last_error = EXCLUDED.last_error,
    updated_at = clock_timestamp();

-- name: MarkOutboxRelayDeadLettered :exec
-- Permanently parks a row after exceeding the retry budget. The pending
-- partial index excludes dead_lettered rows so they will not be re-fetched.
INSERT INTO outbox_svix_relays (outbox_id, attempts, last_error, dead_lettered)
VALUES ($1, 1, $2, TRUE)
ON CONFLICT (outbox_id) DO UPDATE SET
    attempts = outbox_svix_relays.attempts + 1,
    last_error = EXCLUDED.last_error,
    dead_lettered = TRUE,
    updated_at = clock_timestamp();
