-- name: CreateCustomDomain :one
INSERT INTO custom_domains (
    organization_id,
    domain,
    ingress_name,
    cert_secret_name
) VALUES (
    @organization_id,
    @domain,
    @ingress_name,
    @cert_secret_name
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


-- name: UpdateCustomDomain :one
UPDATE custom_domains
SET
    verified = COALESCE(@verified, verified),
    activated = COALESCE(@activated, activated),
    ingress_name = COALESCE(@ingress_name, ingress_name),
    cert_secret_name = COALESCE(@cert_secret_name, cert_secret_name),
    updated_at = clock_timestamp()
WHERE id = @id
  AND deleted IS FALSE
RETURNING *;

-- name: DeleteCustomDomain :exec
UPDATE custom_domains
SET deleted_at = clock_timestamp()
WHERE organization_id = @organization_id
  AND deleted IS FALSE;
