-- name: CreateOpenRouterAPIKey :one
INSERT INTO openrouter_api_keys (
    organization_id
  , key
  , key_hash
  , monthly_credits
) VALUES (
    @organization_id
  , @key
  , @key_hash
  , @monthly_credits
)
RETURNING *;

-- name: GetOpenRouterAPIKey :one
SELECT *
FROM openrouter_api_keys
WHERE organization_id = @organization_id
  AND deleted IS FALSE;

-- name: UpdateOpenRouterKey :one
UPDATE openrouter_api_keys
SET monthly_credits = @monthly_credits
WHERE organization_id = @organization_id
  AND deleted IS FALSE
RETURNING *;
