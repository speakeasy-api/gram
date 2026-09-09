-- name: GetLiveNetworkIngressAuthority :one
SELECT
    id,
    organization_id,
    endpoint_namespace_kind,
    custom_domain_id,
    dns_name
FROM network_ingresses
WHERE id = @id
  AND organization_id = @organization_id
  AND enabled IS TRUE
  AND deleted IS FALSE;

-- name: AcquireNetworkIngressOrganizationLock :exec
-- Serializes lifecycle decisions even when the organization has no ingress row
-- yet, closing concurrent create and create-vs-cleanup gaps.
SELECT pg_advisory_xact_lock(hashtextextended('network-ingress:' || @organization_id::text, 0));

-- name: CreateNetworkIngress :one
INSERT INTO network_ingresses (
    id,
    organization_id,
    provider,
    hostname,
    endpoint_namespace_kind,
    custom_domain_id,
    enabled,
    identity_required,
    credentials_encrypted,
    attestor_namespace,
    attestor_service_account,
    provider_resources
) VALUES (
    @id,
    @organization_id,
    @provider,
    @hostname,
    @endpoint_namespace_kind,
    sqlc.narg('custom_domain_id'),
    @enabled,
    @identity_required,
    @credentials_encrypted,
    @attestor_namespace,
    @attestor_service_account,
    @provider_resources
)
RETURNING *;

-- name: GetNetworkIngressByOrganization :one
SELECT *
FROM network_ingresses
WHERE organization_id = @organization_id
  AND deleted IS FALSE
LIMIT 1;

-- name: GetNetworkIngressByID :one
SELECT *
FROM network_ingresses
WHERE id = @id
  AND organization_id = @organization_id;

-- name: HasEnabledNetworkIngress :one
SELECT EXISTS (
  SELECT 1
  FROM network_ingresses
  WHERE organization_id = @organization_id
    AND enabled IS TRUE
    AND deleted IS FALSE
);

-- name: HasActiveNetworkIngressForCustomDomain :one
SELECT EXISTS (
  SELECT 1
  FROM network_ingresses
  WHERE organization_id = @organization_id
    AND custom_domain_id = @custom_domain_id
    AND deleted IS FALSE
);

-- name: GetPendingDeletedNetworkIngressByOrganization :one
-- Cleanup-critical identities and credentials intentionally survive soft delete.
-- A replacement ingress is blocked until AIS-611 confirms provider deletion and
-- clears both fields.
SELECT *
FROM network_ingresses
WHERE organization_id = @organization_id
  AND deleted IS TRUE
  AND (
    credentials_encrypted IS NOT NULL
    OR provider_resources <> '{}'::jsonb
  )
ORDER BY deleted_at DESC, id DESC
LIMIT 1;

-- name: LockNetworkIngressByOrganization :one
SELECT *
FROM network_ingresses
WHERE organization_id = @organization_id
  AND deleted IS FALSE
LIMIT 1
FOR UPDATE;

-- name: LockNetworkIngressRowsByOrganization :many
-- Serializes active/tombstone lifecycle decisions when rows already exist.
SELECT *
FROM network_ingresses
WHERE organization_id = @organization_id
ORDER BY created_at, id
FOR UPDATE;

-- name: UpdateNetworkIngressSettings :one
UPDATE network_ingresses
SET
    hostname = CASE WHEN @update_hostname::boolean THEN @hostname ELSE hostname END,
    enabled = CASE WHEN @update_enabled::boolean THEN @enabled ELSE enabled END,
    identity_required = CASE WHEN @update_identity_required::boolean THEN @identity_required ELSE identity_required END,
    status = CASE
      WHEN (@update_hostname::boolean OR @update_identity_required::boolean OR (@update_enabled::boolean AND @enabled)) THEN 'pending'
      WHEN (@update_enabled::boolean AND NOT @enabled) THEN 'disabled'
      ELSE status
    END,
    last_error = CASE
      WHEN (@update_hostname::boolean OR @update_identity_required::boolean OR @update_enabled::boolean) THEN NULL
      ELSE last_error
    END,
    updated_at = clock_timestamp()
WHERE organization_id = @organization_id
  AND deleted IS FALSE
RETURNING *;

-- name: RotateNetworkIngressCredentials :one
UPDATE network_ingresses
SET
    credentials_encrypted = @credentials_encrypted,
    status = 'pending',
    last_error = NULL,
    updated_at = clock_timestamp()
WHERE organization_id = @organization_id
  AND deleted IS FALSE
RETURNING *;

-- name: SoftDeleteNetworkIngress :one
UPDATE network_ingresses
SET
    enabled = FALSE,
    status = 'deleting',
    deleted_at = clock_timestamp(),
    updated_at = clock_timestamp()
WHERE organization_id = @organization_id
  AND deleted IS FALSE
RETURNING *;

-- name: CountNetworkIngressDeleteImpact :one
SELECT
  (
    SELECT COUNT(*)
    FROM mcp_servers AS s
    JOIN projects AS p ON p.id = s.project_id AND p.deleted IS FALSE
    WHERE p.organization_id = @organization_id
      AND s.deleted IS FALSE
      AND s.network_access_mode = 'dual'
  )::bigint AS mcp_servers_dual,
  (
    SELECT COUNT(*)
    FROM mcp_servers AS s
    JOIN projects AS p ON p.id = s.project_id AND p.deleted IS FALSE
    WHERE p.organization_id = @organization_id
      AND s.deleted IS FALSE
      AND s.network_access_mode = 'private_only'
  )::bigint AS mcp_servers_private_only,
  (
    SELECT COUNT(*)
    FROM meta_mcp_servers AS s
    JOIN projects AS p ON p.id = s.project_id AND p.deleted IS FALSE
    WHERE s.organization_id = @organization_id
      AND s.deleted IS FALSE
      AND s.network_access_mode = 'dual'
  )::bigint AS meta_mcp_servers_dual,
  (
    SELECT COUNT(*)
    FROM meta_mcp_servers AS s
    JOIN projects AS p ON p.id = s.project_id AND p.deleted IS FALSE
    WHERE s.organization_id = @organization_id
      AND s.deleted IS FALSE
      AND s.network_access_mode = 'private_only'
  )::bigint AS meta_mcp_servers_private_only;

-- name: ListNetworkIngressReconcileRequests :many
SELECT message FROM publish_outbox
WHERE organization_id = @organization_id
  AND topic = 'gram.networkingress.v1.ReconcileRequested'
ORDER BY id;

-- name: GetNetworkIngressForReconcile :one
SELECT * FROM network_ingresses WHERE id = @id;

-- name: LockNetworkIngressForReconcile :one
SELECT * FROM network_ingresses WHERE id = @id FOR UPDATE;

-- name: TryAcquireNetworkIngressReconcileLock :one
-- Session-scoped and separate from the short organization lifecycle lock.
SELECT pg_try_advisory_lock(hashtextextended('network-ingress-reconcile:' || @lock_key::text, 0))::boolean AS acquired;

-- name: ReleaseNetworkIngressReconcileLock :one
SELECT pg_advisory_unlock(hashtextextended('network-ingress-reconcile:' || @lock_key::text, 0))::boolean AS released;

-- name: RecordNetworkIngressObservation :execrows
-- updated_at is the desired-state version, not the observation timestamp.
UPDATE network_ingresses
SET
    status = CASE WHEN enabled THEN @status::text ELSE 'disabled' END,
    dns_name = sqlc.narg('dns_name'),
    last_error = sqlc.narg('last_error'),
    health_checked_at = clock_timestamp(),
    connected_since = CASE
      WHEN enabled AND @status::text = 'online' THEN COALESCE(connected_since, clock_timestamp())
      ELSE NULL
    END
WHERE id = @id
  AND updated_at = @expected_updated_at
  AND deleted IS FALSE;

-- name: ListDueNetworkIngresses :many
SELECT id, organization_id, provider, deleted_at
FROM network_ingresses
WHERE id > @after_id::uuid
  AND (
    (deleted IS TRUE AND (credentials_encrypted IS NOT NULL OR provider_resources <> '{}'::jsonb))
    OR (deleted IS FALSE AND (
      health_checked_at IS NULL
      OR health_checked_at < @stale_before::timestamptz
      OR health_checked_at < updated_at
      OR status IN ('pending', 'error', 'degraded')
      OR last_error IS NOT NULL
    ))
  )
ORDER BY id
LIMIT LEAST(GREATEST(@page_size::integer, 1), 1000);

-- name: ListPersistedNetworkIngressResources :many
SELECT id, provider, provider_resources
FROM network_ingresses
WHERE id > @after_id::uuid
  AND provider_resources <> '{}'::jsonb
ORDER BY id
LIMIT LEAST(GREATEST(@page_size::integer, 1), 1000);

-- name: ClearDeletedNetworkIngressResources :execrows
-- AIS-611 calls this only after every persisted provider resource is confirmed
-- absent. Clearing both fields is the replacement-create release boundary.
UPDATE network_ingresses
SET
    credentials_encrypted = NULL,
    provider_resources = '{}'::jsonb,
    updated_at = clock_timestamp()
WHERE id = @id
  AND organization_id = @organization_id
  AND deleted IS TRUE;
