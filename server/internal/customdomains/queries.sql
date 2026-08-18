-- name: CreateCustomDomain :one
INSERT INTO custom_domains (
    organization_id,
    domain,
    ingress_name,
    cert_secret_name,
    provisioner_kind,
    ip_allowlist
) VALUES (
    @organization_id,
    @domain,
    @ingress_name,
    @cert_secret_name,
    @provisioner_kind,
    @ip_allowlist
)
RETURNING *;

-- name: GetCustomDomainByOrganization :one
SELECT *
FROM custom_domains
WHERE organization_id = @organization_id
  AND deleted IS FALSE
LIMIT 1;

-- name: GetPendingDeletedCustomDomainByOrganization :one
SELECT *
FROM custom_domains
WHERE organization_id = @organization_id
  AND deleted IS TRUE
  AND ingress_name IS NOT NULL
  AND ingress_name <> ''
ORDER BY deleted_at DESC, id DESC
LIMIT 1;

-- name: LockCustomDomainByOrganization :one
SELECT *
FROM custom_domains
WHERE organization_id = @organization_id
  AND deleted IS FALSE
LIMIT 1
FOR UPDATE;

-- name: GetCustomDomainByDomain :one
SELECT *
FROM custom_domains
WHERE domain = @domain
  AND deleted IS FALSE;

-- name: GetCustomDomainByID :one
SELECT *
FROM custom_domains
WHERE id = @id
  AND deleted IS FALSE;

-- name: LockCustomDomainByID :one
-- Mutations acquire the domain lock before endpoint/server rows.
SELECT id
FROM custom_domains
WHERE id = @id
  AND deleted IS FALSE
FOR UPDATE;

-- name: GetCustomDomainRouteConfig :one
-- Canonical desired-state read for reconciliation and health. Keep root
-- eligibility here so both paths agree on whether the secondary route exists.
SELECT
    d.id,
    d.organization_id,
    d.domain,
    d.verified,
    d.activated,
    d.ingress_name,
    d.cert_secret_name,
    d.provisioner_kind,
    d.ip_allowlist,
    d.deleted,
    COALESCE(root_endpoint.id, '00000000-0000-0000-0000-000000000000'::uuid) AS root_mcp_endpoint_id,
    COALESCE(root_endpoint.slug, '')::text AS root_slug
FROM custom_domains AS d
LEFT JOIN LATERAL (
    SELECT e.id, e.slug
    FROM mcp_endpoints AS e
    JOIN projects AS p
      ON p.id = e.project_id
     AND p.deleted IS FALSE
    JOIN mcp_servers AS s
      ON s.id = e.mcp_server_id
     AND s.deleted IS FALSE
     AND s.visibility <> 'disabled'
    WHERE e.custom_domain_id = d.id
      AND e.is_domain_root IS TRUE
      AND e.deleted IS FALSE
    LIMIT 1
) AS root_endpoint ON TRUE
WHERE d.id = @id;

-- name: LockRootMcpEndpointSelection :many
-- The caller locks the parent custom-domain row first. Sort endpoint locks to
-- keep replacement and lifecycle mutations on one global lock order.
SELECT id
FROM mcp_endpoints
WHERE custom_domain_id = @custom_domain_id::uuid
  AND deleted IS FALSE
  AND (
    is_domain_root IS TRUE
    OR id = sqlc.narg('mcp_endpoint_id')::uuid
  )
ORDER BY id
FOR UPDATE;

-- name: GetEligibleRootMcpEndpoint :one
SELECT e.*
FROM mcp_endpoints AS e
JOIN projects AS p
  ON p.id = e.project_id
 AND p.deleted IS FALSE
JOIN mcp_servers AS s
  ON s.id = e.mcp_server_id
 AND s.deleted IS FALSE
 AND s.visibility <> 'disabled'
WHERE e.id = @mcp_endpoint_id::uuid
  AND e.custom_domain_id = @custom_domain_id::uuid
  AND e.deleted IS FALSE
  AND p.organization_id = @organization_id
FOR SHARE OF s;

-- name: ClearRootMcpEndpoint :exec
UPDATE mcp_endpoints
SET
    is_domain_root = NULL,
    updated_at = clock_timestamp()
WHERE custom_domain_id = @custom_domain_id::uuid
  AND is_domain_root IS TRUE
  AND deleted IS FALSE;

-- name: SetRootMcpEndpoint :exec
UPDATE mcp_endpoints
SET
    is_domain_root = TRUE,
    updated_at = clock_timestamp()
WHERE id = @mcp_endpoint_id::uuid
  AND custom_domain_id = @custom_domain_id::uuid
  AND deleted IS FALSE;

-- name: GetCustomDomainByIDAndOrganization :one
-- Organization-scoped variant of GetCustomDomainByID. Use this when the caller
-- has an organization context and needs to enforce that the custom domain
-- belongs to it (e.g. ownership checks on referenced custom_domain_id columns
-- in other tables).
SELECT *
FROM custom_domains
WHERE id = @id
  AND organization_id = @organization_id
  AND deleted IS FALSE;

-- name: ListActivatedCustomDomainsForHealthCheck :many
SELECT id, organization_id
FROM custom_domains
WHERE activated IS TRUE
  AND deleted IS FALSE
  AND id > @after_id
ORDER BY id
LIMIT @page_limit;

-- name: ListActivatedCustomDomainResources :many
-- Internal system-wide orphan sweep. Intentionally spans all organizations;
-- not reachable from user-facing handlers. Keep the root-mapping predicate in
-- sync with GetCustomDomainRouteConfig so sweep and reconciler agree.
SELECT
    d.id,
    d.domain,
    d.provisioner_kind,
    COALESCE(d.ingress_name, '')::text AS resource_name,
    EXISTS (
        SELECT 1
        FROM mcp_endpoints AS e
        JOIN projects AS p
          ON p.id = e.project_id
         AND p.deleted IS FALSE
        JOIN mcp_servers AS s
          ON s.id = e.mcp_server_id
         AND s.deleted IS FALSE
         AND s.visibility <> 'disabled'
        WHERE e.custom_domain_id = d.id
          AND e.is_domain_root IS TRUE
          AND e.deleted IS FALSE
    ) AS has_root_mapping
FROM custom_domains AS d
WHERE d.activated IS TRUE
  AND d.ingress_name IS NOT NULL
  AND d.deleted IS FALSE;

-- name: GetOrganizationSlugForHealthNotification :one
SELECT slug
FROM organization_metadata
WHERE id = @organization_id;

-- name: LockCustomDomainByIDAndOrganization :one
-- Org-scoped row lock for mutations that target one custom domain by id
-- (root-mapping updates, health writes). Domain lock comes before any
-- endpoint/server locks.
SELECT *
FROM custom_domains
WHERE id = @id
  AND organization_id = @organization_id
  AND deleted IS FALSE
FOR UPDATE;

-- name: UpdateCustomDomainHealth :one
UPDATE custom_domains
SET
    health_status = @health_status,
    health_issue = @health_issue,
    health_checked_at = @checked_at,
    unhealthy_since = @unhealthy_since,
    certificate_expires_at = @certificate_expires_at,
    consecutive_failures = @consecutive_failures,
    updated_at = clock_timestamp()
WHERE id = @id
  AND organization_id = @organization_id
  AND deleted IS FALSE
RETURNING *;


-- name: UpdateCustomDomain :one
UPDATE custom_domains
SET
    verified = COALESCE(@verified, verified),
    activated = COALESCE(@activated, activated),
    ingress_name = COALESCE(@ingress_name, ingress_name),
    cert_secret_name = COALESCE(@cert_secret_name, cert_secret_name),
    provisioner_kind = @provisioner_kind,
    updated_at = clock_timestamp()
WHERE id = @id
  AND deleted IS FALSE
RETURNING *;

-- name: UpdateCustomDomainResourceNames :one
-- Resource identity must survive a concurrent soft delete so the reconciler
-- can remove an Apply that completed after deletion began.
UPDATE custom_domains
SET
    ingress_name = @ingress_name,
    cert_secret_name = @cert_secret_name,
    provisioner_kind = @provisioner_kind,
    updated_at = clock_timestamp()
WHERE id = @id
RETURNING *;

-- name: EnsureCustomDomainResourceNames :execrows
-- Deletion checkpoint: fill derived resource identity when Apply never
-- persisted one, so the tombstone stays discoverable for cleanup retries.
-- COALESCE keeps identity persisted by a real Apply. Active rows only — a
-- cleaned tombstone must never be repopulated (its derived names may belong
-- to a successor domain reusing the hostname).
UPDATE custom_domains
SET
    ingress_name = COALESCE(NULLIF(ingress_name, ''), @ingress_name),
    cert_secret_name = COALESCE(NULLIF(cert_secret_name, ''), @cert_secret_name),
    updated_at = clock_timestamp()
WHERE id = @id
  AND organization_id = @organization_id
  AND deleted IS FALSE;

-- name: ClearDeletedCustomDomainResourceNames :exec
UPDATE custom_domains
SET
    ingress_name = NULL,
    cert_secret_name = NULL,
    updated_at = clock_timestamp()
WHERE id = @id
  AND deleted IS TRUE;

-- name: UpdateCustomDomainIPAllowlist :one
UPDATE custom_domains
SET
    ip_allowlist = @ip_allowlist,
    updated_at = clock_timestamp()
WHERE organization_id = @organization_id
  AND deleted IS FALSE
RETURNING *;

-- name: UpdateCustomDomainSettings :one
UPDATE custom_domains
SET
    ip_allowlist = CASE
        WHEN @update_ip_allowlist::boolean THEN @ip_allowlist::text[]
        ELSE ip_allowlist
    END,
    openai_apps_challenge_token = CASE
        WHEN @update_openai_apps_challenge_token::boolean THEN sqlc.narg('openai_apps_challenge_token')::text
        ELSE openai_apps_challenge_token
    END,
    updated_at = clock_timestamp()
WHERE organization_id = @organization_id
  AND deleted IS FALSE
RETURNING *;

-- name: DeleteCustomDomain :exec
UPDATE custom_domains
SET deleted_at = clock_timestamp()
WHERE organization_id = @organization_id
  AND deleted IS FALSE;

-- name: DisableCustomDomainForHealth :one
-- Clearing activated drops the domain from health sweeps; clearing verified
-- puts it back into the dashboard reverify flow. Caller tears down k8s.
UPDATE custom_domains
SET
    verified = FALSE,
    activated = FALSE,
    updated_at = clock_timestamp()
WHERE id = @id
  AND organization_id = @organization_id
  AND deleted IS FALSE
RETURNING id;
