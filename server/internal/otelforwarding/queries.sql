-- name: GetOrgOTELForwardingConfig :one
SELECT *
FROM otel_forwarding_configs
WHERE organization_id = @organization_id
  AND project_id IS NULL
  AND deleted IS FALSE;

-- name: UpsertOrgOTELForwardingConfig :one
INSERT INTO otel_forwarding_configs (
    organization_id
  , endpoint_url
  , headers_encrypted
  , enabled
) VALUES (
    @organization_id
  , @endpoint_url
  , @headers_encrypted
  , @enabled
)
ON CONFLICT (organization_id) WHERE project_id IS NULL AND deleted IS FALSE
DO UPDATE SET
    endpoint_url = EXCLUDED.endpoint_url
  , headers_encrypted = EXCLUDED.headers_encrypted
  , enabled = EXCLUDED.enabled
  , updated_at = clock_timestamp()
RETURNING *;

-- name: SoftDeleteOrgOTELForwardingConfig :exec
UPDATE otel_forwarding_configs
SET deleted_at = clock_timestamp()
WHERE organization_id = @organization_id
  AND project_id IS NULL
  AND deleted IS FALSE;

-- name: GetAPIKeyOrgByKeyHash :one
-- Lightweight org-id lookup used by the OTEL forwarding middleware. Avoids
-- the JOIN that the auth package's GetAPIKeyByKeyHash performs.
SELECT organization_id
FROM api_keys
WHERE key_hash = @key_hash
  AND deleted IS FALSE;
