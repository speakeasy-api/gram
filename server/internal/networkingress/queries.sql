-- name: GetLiveNetworkIngressAuthority :one
SELECT
    id,
    organization_id,
    endpoint_namespace_kind,
    custom_domain_id,
    dns_name
FROM network_ingresses
WHERE id = @id
  AND enabled IS TRUE
  AND deleted IS FALSE;
