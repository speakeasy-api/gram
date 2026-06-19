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
SET monthly_credits = @monthly_credits, key_hash = @key_hash, key = @key
WHERE organization_id = @organization_id
  AND deleted IS FALSE
RETURNING *;

-- name: UpdateOpenRouterKeyMonthlyCredits :exec
-- Updates only monthly_credits for the given organization. Used by the
-- metrics-collection reconciliation path when the upstream OpenRouter limit
-- diverges from the locally cached value (e.g. after a manual change on the
-- OpenRouter dashboard). Distinct from UpdateOpenRouterKey, which is the
-- key-provisioning write path and also mutates key/key_hash.
UPDATE openrouter_api_keys
SET monthly_credits = @monthly_credits
WHERE organization_id = @organization_id
  AND deleted IS FALSE;
