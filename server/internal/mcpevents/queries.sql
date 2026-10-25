-- name: GetSubscription :one
SELECT * FROM platform_mcp_event_subscriptions
WHERE id = @id AND project_id = @project_id AND organization_id = @organization_id;

-- name: SaveSubscription :one
INSERT INTO platform_mcp_event_subscriptions (
 id, project_id, organization_id, connection_id, connection_generation, client_id, user_id,
 event_name, callback_url, signing_secret, previous_signing_secret, previous_secret_expires_at,
 generation, subscribed_at, expires_at, verified_at
) VALUES (
 @id, @project_id, @organization_id, @connection_id, @connection_generation, @client_id, @user_id,
 @event_name, @callback_url, @signing_secret, sqlc.narg(previous_signing_secret), sqlc.narg(previous_secret_expires_at),
 @generation, @subscribed_at, @expires_at, @verified_at
)
ON CONFLICT (id) DO UPDATE SET
 signing_secret = EXCLUDED.signing_secret,
 previous_signing_secret = EXCLUDED.previous_signing_secret,
 previous_secret_expires_at = EXCLUDED.previous_secret_expires_at,
 generation = EXCLUDED.generation,
 subscribed_at = EXCLUDED.subscribed_at,
 expires_at = EXCLUDED.expires_at,
 verified_at = EXCLUDED.verified_at,
 deleted_at = NULL,
 updated_at = clock_timestamp()
WHERE platform_mcp_event_subscriptions.project_id = @project_id
 AND platform_mcp_event_subscriptions.organization_id = @organization_id
RETURNING *;

-- name: LockSubscriptionIdentity :exec
SELECT pg_advisory_xact_lock(hashtextextended(@identity::text, 0));

-- name: CountActiveSubscriptions :one
SELECT count(*) FROM platform_mcp_event_subscriptions
WHERE project_id = @project_id AND organization_id = @organization_id
 AND connection_id = @connection_id AND connection_generation = @connection_generation
 AND deleted_at IS NULL AND expires_at > @now;

-- name: ListEventDeliveries :many
SELECT * FROM platform_mcp_event_deliveries
WHERE project_id = @project_id AND organization_id = @organization_id AND event_id = @event_id
ORDER BY id;

-- name: Unsubscribe :exec
UPDATE platform_mcp_event_subscriptions SET deleted_at = @now, updated_at = @now
WHERE id = @id AND project_id = @project_id AND organization_id = @organization_id;

-- name: MatchSubscriptions :many
SELECT * FROM platform_mcp_event_subscriptions
WHERE organization_id = @organization_id AND project_id = @project_id
 AND event_name = @event_name AND deleted_at IS NULL
 AND subscribed_at <= @occurred_at AND expires_at > @now
 AND (@event_name::text = 'review.requested' OR user_id = ANY(@recipients::text[]))
ORDER BY id;

-- name: CreateDelivery :one
INSERT INTO platform_mcp_event_deliveries (
 id, project_id, organization_id, subscription_id, subscription_generation,
 event_id, event_name, request_id, payload, status
) VALUES (
 @id, @project_id, @organization_id, @subscription_id, @subscription_generation,
 @event_id, @event_name, @request_id, @payload, 'pending'
)
ON CONFLICT (subscription_id, subscription_generation, event_id) DO NOTHING
RETURNING id;

-- name: GetDeliveryForUpdate :one
SELECT * FROM platform_mcp_event_deliveries
WHERE id = @id AND project_id = @project_id AND organization_id = @organization_id
FOR UPDATE;

-- name: SetDeliveryOutcome :exec
UPDATE platform_mcp_event_deliveries SET status = @status, attempts = attempts + 1, updated_at = @now
WHERE id = @id AND project_id = @project_id AND organization_id = @organization_id;

-- name: ListReviewEventRecipients :many
SELECT user_id FROM mcp_approval_request_requesters
WHERE mcp_approval_request_id = @request_id AND project_id = @project_id
 AND organization_id = @organization_id AND deleted IS FALSE
ORDER BY user_id;

-- These worker-only, bounded maintenance queries intentionally span tenants.
-- name: CleanupExpiredEventSubscriptions :execrows
DELETE FROM platform_mcp_event_subscriptions WHERE (organization_id, project_id, id) IN (
 SELECT stale.organization_id, stale.project_id, stale.id FROM platform_mcp_event_subscriptions AS stale
 WHERE least(stale.expires_at, stale.deleted_at) < @cutoff
 AND NOT EXISTS (SELECT 1 FROM platform_mcp_event_deliveries AS delivery WHERE delivery.subscription_id = stale.id AND delivery.organization_id = stale.organization_id AND delivery.project_id = stale.project_id)
 ORDER BY least(stale.expires_at, stale.deleted_at), stale.id
 LIMIT @batch_size FOR UPDATE SKIP LOCKED
);

-- name: CleanupOldEventDeliveries :execrows
DELETE FROM platform_mcp_event_deliveries WHERE (organization_id, project_id, id) IN (
 SELECT stale.organization_id, stale.project_id, stale.id FROM platform_mcp_event_deliveries AS stale WHERE stale.created_at < @cutoff
 ORDER BY stale.created_at, stale.id LIMIT @batch_size FOR UPDATE SKIP LOCKED
);

-- Detach retained delivery history in bounded batches before deleting parents;
-- otherwise ON DELETE SET NULL can turn one parent deletion into unbounded writes.
-- name: DetachExpiredEventSubscriptions :execrows
UPDATE platform_mcp_event_deliveries SET subscription_id = NULL WHERE (organization_id, project_id, id) IN (
 SELECT delivery.organization_id, delivery.project_id, delivery.id FROM platform_mcp_event_deliveries AS delivery
 JOIN platform_mcp_event_subscriptions AS subscription ON subscription.id = delivery.subscription_id AND subscription.organization_id = delivery.organization_id AND subscription.project_id = delivery.project_id
 WHERE least(subscription.expires_at, subscription.deleted_at) < @cutoff
 ORDER BY delivery.id LIMIT @batch_size FOR UPDATE OF delivery SKIP LOCKED
);

-- name: GetReviewEventRequest :one
SELECT id FROM mcp_approval_requests
WHERE id = @id AND project_id = @project_id AND organization_id = @organization_id AND deleted IS FALSE;
