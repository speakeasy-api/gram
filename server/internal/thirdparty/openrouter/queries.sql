-- name: CreateOpenRouterAPIKey :one
INSERT INTO openrouter_api_keys (
    organization_id
  , key
  , key_hash
) VALUES (
    @organization_id
  , @key
  , @key_hash
)
RETURNING *;

-- name: GetOpenRouterAPIKey :one
SELECT *
FROM openrouter_api_keys
WHERE organization_id = @organization_id
  AND deleted IS FALSE;
