-- name: LockOpenRouterKeyProvisioning :exec
-- Serialize first-time key creation per (org, key type) so concurrent
-- completions cannot both mint an upstream OpenRouter key.
SELECT pg_advisory_xact_lock(hashtext('openrouter_key:' || @organization_id::text || ':' || @key_type::text));

-- name: CreateOpenRouterAPIKey :one
INSERT INTO openrouter_api_keys (
    organization_id
  , key_type
  , key_encrypted
  , key_hash
  , monthly_credits
) VALUES (
    @organization_id
  , @key_type
  , @key_encrypted
  , @key_hash
  , @monthly_credits
)
RETURNING *;

-- name: GetOpenRouterAPIKey :one
SELECT *
FROM openrouter_api_keys
WHERE organization_id = @organization_id
  AND key_type = @key_type
  AND deleted IS FALSE;

-- name: UpdateOpenRouterKey :one
UPDATE openrouter_api_keys
SET monthly_credits = @monthly_credits, key_hash = @key_hash,
    disabled = disabled AND NOT @reinstate::boolean
WHERE organization_id = @organization_id
  AND key_type = @key_type
  AND deleted IS FALSE
RETURNING *;

-- name: DisableOpenRouterAPIKey :exec
-- Locks the key down without deleting it, so a reinstated organization keeps
-- the same upstream key and its ceiling. ProvisionAPIKey reads this flag and
-- refuses to hand the key to a completion.
UPDATE openrouter_api_keys
SET disabled = TRUE,
    updated_at = clock_timestamp()
WHERE organization_id = @organization_id
  AND key_type = @key_type
  AND deleted IS FALSE;

-- name: UpdateOpenRouterKeyMonthlyCredits :exec
-- Updates only monthly_credits for the given organization. Used by the
-- metrics-collection reconciliation path when the upstream OpenRouter limit
-- diverges from the locally cached value (e.g. after a manual change on the
-- OpenRouter dashboard). Distinct from UpdateOpenRouterKey, which is the
-- key-provisioning write path and also mutates key_hash.
UPDATE openrouter_api_keys
SET monthly_credits = @monthly_credits
WHERE organization_id = @organization_id
  AND key_type = @key_type
  AND deleted IS FALSE;
