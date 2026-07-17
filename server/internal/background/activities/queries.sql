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
-- OpenrouterKeyRefreshWorkflow. The api_key is included so the caller can
-- issue the upstream usage HTTP call in a single round-trip — keep it inside
-- the activity boundary and never return it to the workflow.
SELECT
    om.id AS organization_id,
    om.slug AS organization_slug,
    om.gram_account_type,
    k.key_type,
    k.monthly_credits,
    k.key AS api_key
FROM organization_metadata om
JOIN openrouter_api_keys k ON k.organization_id = om.id
WHERE om.disabled_at IS NULL
  AND k.disabled = FALSE
  AND k.deleted = FALSE
  AND om.gram_account_type = ANY(@account_types::text[])
ORDER BY om.slug;

-- name: GetOpenRouterCreditsAlertRecipients :many
-- Resolve the billing alert recipient for each supplied organization that
-- should receive an OpenRouter credit threshold warning. An org qualifies only
-- if it has a billing alert email configured (the address set on the billing
-- page) and is not using BYOK — any enabled, non-deleted customer-supplied
-- model provider key means the platform chat key is not what pays for the org's
-- completions, so credit exhaustion of that key is not customer-facing and no
-- warning is sent. Orgs without a recipient or with BYOK are simply omitted.
SELECT
    om.id AS organization_id,
    om.name AS organization_name,
    bm.alert_email
FROM organization_metadata om
JOIN billing_metadata bm ON bm.organization_id = om.id
WHERE om.id = ANY(@organization_ids::text[])
  AND bm.alert_email IS NOT NULL
  AND NOT EXISTS (
      SELECT 1
      FROM model_provider_keys mpk
      WHERE mpk.organization_id = om.id
        AND mpk.enabled = TRUE
        AND mpk.deleted = FALSE
  );

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
  AND (project_id IS NULL OR project_id = @project_id)
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
  AND (project_id IS NULL OR project_id = @project_id)
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
