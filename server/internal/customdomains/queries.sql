-- name: CreateCustomDomain :one
INSERT INTO custom_domains (
    project_id,
    domain,
    ingress_name,
    cert_secret_name
) VALUES (
    @project_id,
    @domain,
    @ingress_name,
    @cert_secret_name
)
RETURNING *;

-- name: GetCustomDomainsByProject :many
SELECT *
FROM custom_domains
WHERE project_id = @project_id
  AND deleted IS FALSE
ORDER BY created_at DESC;

-- name: GetCustomDomainByDomain :one
SELECT *
FROM custom_domains
WHERE domain = @domain
  AND deleted IS FALSE;

-- name: UpdateCustomDomain :one
UPDATE custom_domains
SET
    verified = COALESCE(@verified, verified),
    ingress_name = COALESCE(@ingress_name, ingress_name),
    cert_secret_name = COALESCE(@cert_secret_name, cert_secret_name),
    updated_at = clock_timestamp()
WHERE id = @id
  AND deleted IS FALSE
RETURNING *;

-- name: DeleteCustomDomain :exec
UPDATE custom_domains
SET deleted_at = clock_timestamp()
WHERE id = @id
  AND deleted IS FALSE;
