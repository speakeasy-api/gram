-- name: ListDesiredGatewayIngresses :many
-- Enabled ingresses served by the tsnet gateway for one shard. Gateway pods
-- poll this to decide which hosts to run.
SELECT
    i.id,
    i.organization_id,
    om.slug AS organization_slug,
    i.hostname,
    i.identity_required,
    i.updated_at
FROM network_ingresses i
JOIN organization_metadata om ON om.id = i.organization_id
WHERE i.enabled IS TRUE
  AND i.deleted IS FALSE
  AND i.runtime = 'gateway'
  AND i.gateway_shard = @gateway_shard
ORDER BY i.id;

-- name: GetNetworkIngressNodeSlot :one
SELECT *
FROM network_ingress_nodes
WHERE network_ingress_id = @network_ingress_id
  AND organization_id = @organization_id
  AND slot = @slot;

-- name: ListNetworkIngressNodes :many
SELECT *
FROM network_ingress_nodes
WHERE network_ingress_id = @network_ingress_id
  AND organization_id = @organization_id
ORDER BY slot;

-- name: EnsureNetworkIngressNodeSlot :exec
INSERT INTO network_ingress_nodes (network_ingress_id, organization_id, slot)
VALUES (@network_ingress_id, @organization_id, @slot)
ON CONFLICT (network_ingress_id, slot) DO NOTHING;

-- name: SetNetworkIngressNodeAuthKey :execrows
-- Installs a fresh one-shot auth key for a slot that has no persisted node
-- state yet. Retired slots never receive keys.
UPDATE network_ingress_nodes
SET auth_key_encrypted = @auth_key_encrypted,
    auth_key_expires_at = @auth_key_expires_at,
    status = 'pending',
    last_error = NULL,
    updated_at = clock_timestamp()
WHERE network_ingress_id = @network_ingress_id
  AND organization_id = @organization_id
  AND slot = @slot
  AND retired_at IS NULL;

-- name: ClearNetworkIngressNodeAuthKey :exec
UPDATE network_ingress_nodes
SET auth_key_encrypted = NULL,
    auth_key_expires_at = NULL,
    updated_at = clock_timestamp()
WHERE network_ingress_id = @network_ingress_id
  AND organization_id = @organization_id
  AND slot = @slot;

-- name: HeartbeatNetworkIngressNode :execrows
-- Written by the gateway pod hosting the slot. updated_at tracks the latest
-- heartbeat; the reconciler treats stale last_seen_at as an offline host.
UPDATE network_ingress_nodes
SET pod_name = sqlc.narg('pod_name'),
    status = @status,
    node_id = COALESCE(sqlc.narg('node_id'), node_id),
    dns_name = COALESCE(sqlc.narg('dns_name'), dns_name),
    last_error = sqlc.narg('last_error'),
    last_seen_at = clock_timestamp(),
    connected_since = CASE
      WHEN @status::text = 'online' THEN COALESCE(connected_since, clock_timestamp())
      ELSE NULL
    END,
    updated_at = clock_timestamp()
WHERE network_ingress_id = @network_ingress_id
  AND organization_id = @organization_id
  AND slot = @slot;

-- name: RetireNetworkIngressNodes :exec
-- Marks every slot at or above min_slot as retired so gateway pods stop
-- hosting it and the reconciler deletes its device.
UPDATE network_ingress_nodes
SET retired_at = COALESCE(retired_at, clock_timestamp()),
    updated_at = clock_timestamp()
WHERE network_ingress_id = @network_ingress_id
  AND organization_id = @organization_id
  AND slot >= @min_slot;

-- name: DeleteNetworkIngressNodes :exec
-- Hard delete: the devices have been removed from the tailnet, so the slot
-- identities must not be reused by a later host.
DELETE FROM network_ingress_nodes
WHERE network_ingress_id = @network_ingress_id
  AND organization_id = @organization_id;

-- name: GetNetworkIngressNodeState :one
SELECT value_encrypted
FROM network_ingress_node_states
WHERE network_ingress_id = @network_ingress_id
  AND organization_id = @organization_id
  AND slot = @slot
  AND state_key = @state_key;

-- name: UpsertNetworkIngressNodeState :exec
INSERT INTO network_ingress_node_states (network_ingress_id, organization_id, slot, state_key, value_encrypted)
VALUES (@network_ingress_id, @organization_id, @slot, @state_key, @value_encrypted)
ON CONFLICT (network_ingress_id, slot, state_key) DO UPDATE
SET value_encrypted = EXCLUDED.value_encrypted,
    updated_at = clock_timestamp();

-- name: DeleteNetworkIngressNodeState :exec
DELETE FROM network_ingress_node_states
WHERE network_ingress_id = @network_ingress_id
  AND organization_id = @organization_id
  AND slot = @slot
  AND state_key = @state_key;

-- name: HasNetworkIngressNodeState :one
SELECT EXISTS (
  SELECT 1
  FROM network_ingress_node_states
  WHERE network_ingress_id = @network_ingress_id
    AND organization_id = @organization_id
    AND slot = @slot
);

-- name: DeleteNetworkIngressNodeStates :exec
-- Hard delete: the node identities are gone from the tailnet.
DELETE FROM network_ingress_node_states
WHERE network_ingress_id = @network_ingress_id
  AND organization_id = @organization_id;

-- name: MarkNetworkIngressRuntime :execrows
-- updated_at is the desired-state version and is deliberately not touched.
UPDATE network_ingresses
SET runtime = @runtime
WHERE id = @id
  AND organization_id = @organization_id
  AND deleted IS FALSE
  AND runtime IS DISTINCT FROM @runtime;

-- name: GetNetworkIngressRuntime :one
SELECT runtime
FROM network_ingresses
WHERE id = @id
  AND organization_id = @organization_id;
