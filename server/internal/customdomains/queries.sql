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
SELECT
    domain,
    provisioner_kind,
    COALESCE(ingress_name, '')::text AS resource_name
FROM custom_domains
WHERE activated IS TRUE
  AND ingress_name IS NOT NULL
  AND deleted IS FALSE;

-- name: ListOrganizationUsersForHealthNotification :many
-- Authorization filtering is applied by the caller.
SELECT users.id, users.email
FROM organization_user_relationships AS our
JOIN users
  ON users.id = our.user_id
WHERE our.organization_id = @organization_id
  AND our.deleted IS FALSE
  AND users.deleted_at IS NULL
  AND users.email <> ''
ORDER BY users.email, users.id;

-- name: GetOrganizationSlugForHealthNotification :one
SELECT slug
FROM organization_metadata
WHERE id = @organization_id;

-- name: GetCustomDomainByIDAndOrganizationForHealthUpdate :one
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

-- name: UpdateCustomDomainIPAllowlist :one
UPDATE custom_domains
SET
    ip_allowlist = @ip_allowlist,
    updated_at = clock_timestamp()
WHERE organization_id = @organization_id
  AND deleted IS FALSE
RETURNING *;

-- name: DeleteCustomDomain :exec
UPDATE custom_domains
SET deleted_at = clock_timestamp()
WHERE organization_id = @organization_id
  AND deleted IS FALSE;
