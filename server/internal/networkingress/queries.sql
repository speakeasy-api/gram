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
WHERE id = @id;

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
      WHEN (@update_hostname::boolean OR (@update_enabled::boolean AND @enabled)) THEN 'pending'
      WHEN (@update_enabled::boolean AND NOT @enabled) THEN 'disabled'
      ELSE status
    END,
    last_error = CASE
      WHEN (@update_hostname::boolean OR @update_enabled::boolean) THEN NULL
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
    WHERE s.organization_id = @organization_id
      AND s.deleted IS FALSE
      AND s.network_access_mode = 'dual'
  )::bigint AS meta_mcp_servers_dual,
  (
    SELECT COUNT(*)
    FROM meta_mcp_servers AS s
    WHERE s.organization_id = @organization_id
      AND s.deleted IS FALSE
      AND s.network_access_mode = 'private_only'
  )::bigint AS meta_mcp_servers_private_only;

-- name: ClearDeletedNetworkIngressResources :execrows
-- AIS-611 calls this only after every persisted provider resource is confirmed
-- absent. Clearing both fields is the replacement-create release boundary.
UPDATE network_ingresses
SET
    credentials_encrypted = NULL,
    provider_resources = '{}'::jsonb,
    updated_at = clock_timestamp()
WHERE id = @id
  AND deleted IS TRUE;
