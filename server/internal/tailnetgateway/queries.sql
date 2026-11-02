-- name: ListDesiredGatewayIngressesForSlot :many
-- Enabled ingresses the gateway pod with this ordinal must host: the ingress
-- is served by the gateway on this shard and its slot row exists and is not
-- retired. A retired or missing slot drops the ingress from the result, which
-- is how a running host learns to stop.
SELECT
    i.id,
    i.organization_id,
    om.slug AS organization_slug,
    i.hostname,
    i.identity_required,
    i.updated_at
FROM network_ingresses i
JOIN organization_metadata om ON om.id = i.organization_id
JOIN network_ingress_nodes n
  ON n.organization_id = i.organization_id
 AND n.network_ingress_id = i.id
 AND n.slot = @slot
 AND n.retired_at IS NULL
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
-- Installs a fresh one-shot auth key for a slot that has not completed a
-- login (no persisted profile list). Retired slots never receive keys.
UPDATE network_ingress_nodes AS n
SET auth_key_encrypted = @auth_key_encrypted,
    auth_key_expires_at = @auth_key_expires_at,
    status = 'pending',
    last_error = NULL,
    updated_at = clock_timestamp()
WHERE n.network_ingress_id = @network_ingress_id
  AND n.organization_id = @organization_id
  AND n.slot = @slot
  AND n.retired_at IS NULL
  AND NOT EXISTS (
    SELECT 1
    FROM network_ingress_node_states s
    WHERE s.network_ingress_id = n.network_ingress_id
      AND s.organization_id = n.organization_id
      AND s.slot = n.slot
      AND s.state_key = '_profiles'
  );

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
-- heartbeat; the reconciler treats stale last_seen_at as an offline host. A
-- retired slot only accepts the draining and stopped beats of the host that
-- is shutting it down, so a late beat can never resurrect it.
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
  AND slot = @slot
  AND (retired_at IS NULL OR @status::text IN ('draining', 'stopped'));

-- name: RetireNetworkIngressNodes :exec
-- Marks every slot at or above min_slot as retired so gateway pods stop
-- hosting it. Retirement is permanent for that node identity: once the host
-- has stopped, the reconciler deletes the device and purges the slot, and a
-- later slot count increase recreates it as a fresh identity.
UPDATE network_ingress_nodes
SET retired_at = COALESCE(retired_at, clock_timestamp()),
    updated_at = clock_timestamp()
WHERE network_ingress_id = @network_ingress_id
  AND organization_id = @organization_id
  AND slot >= @min_slot;

-- name: DeleteNetworkIngressNodeSlot :exec
-- Hard delete of one retired slot after its device left the tailnet.
DELETE FROM network_ingress_nodes
WHERE network_ingress_id = @network_ingress_id
  AND organization_id = @organization_id
  AND slot = @slot
  AND retired_at IS NOT NULL;

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

-- name: DeleteNetworkIngressNodeSlotStates :exec
-- Hard delete of one slot's partial state after a failed first login.
DELETE FROM network_ingress_node_states
WHERE network_ingress_id = @network_ingress_id
  AND organization_id = @organization_id
  AND slot = @slot;

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

-- name: GetNetworkIngressGatewayOwner :one
-- Tombstones are included: teardown needs the owner and label after soft delete.
SELECT organization_id, hostname, runtime
FROM network_ingresses
WHERE id = @id;
