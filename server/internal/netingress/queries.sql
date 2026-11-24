-- name: GetIngressServingState :one
SELECT
    organization_id,
    provider,
    dns_name,
    identity_required,
    enabled,
    deleted
FROM network_ingresses
WHERE id = @id
  AND organization_id = @organization_id;

-- name: GetActiveIngressesByDNSName :many
-- Resolves the ingress a tailnet gateway pod is proxying for from the request
-- Host. Only gateway-runtime ingresses are eligible; callers fail closed
-- unless exactly one row matches.
SELECT
    id,
    organization_id,
    provider,
    dns_name,
    identity_required
FROM network_ingresses
WHERE dns_name = @dns_name::text
  AND runtime = 'gateway'
  AND enabled IS TRUE
  AND deleted IS FALSE
LIMIT 2;
