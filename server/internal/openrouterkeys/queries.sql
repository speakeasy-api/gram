-- name: ListOpenRouterAPIKeysForAdmin :many
-- Platform-admin inventory of every organization's platform OpenRouter keys.
-- Key material is deliberately absent from the select list: this feeds an API
-- response that never carries the secrets themselves.
SELECT
    k.organization_id,
    om.name AS organization_name,
    om.slug AS organization_slug,
    om.gram_account_type,
    k.key_type,
    k.monthly_credits,
    (CASE WHEN k.disable_causes IS NULL THEN k.disabled ELSE cardinality(k.disable_causes) > 0 END)::boolean AS disabled,
    k.disable_causes,
    k.created_at,
    k.updated_at
FROM openrouter_api_keys k
JOIN organization_metadata om ON om.id = k.organization_id
WHERE k.deleted IS FALSE
ORDER BY om.slug, k.key_type;

-- name: GetOpenRouterAPIKeyForAdmin :one
SELECT
    k.organization_id,
    om.name AS organization_name,
    om.slug AS organization_slug,
    om.gram_account_type,
    k.key_type,
    k.monthly_credits,
    (CASE WHEN k.disable_causes IS NULL THEN k.disabled ELSE cardinality(k.disable_causes) > 0 END)::boolean AS disabled,
    k.disable_causes,
    k.created_at,
    k.updated_at
FROM openrouter_api_keys k
JOIN organization_metadata om ON om.id = k.organization_id
WHERE k.organization_id = @organization_id
  AND k.key_type = @key_type
  AND k.deleted IS FALSE;

-- name: GetOrganizationAuditCursor :one
SELECT COALESCE((
  SELECT seq
  FROM audit_logs
  WHERE organization_id = @organization_id
  ORDER BY seq DESC
  LIMIT 1
), 0)::bigint AS cursor;

-- name: GetAdminMutationAuditCursorSince :one
-- Residual action and metadata predicates are evaluated only inside the
-- organization/sequence range captured by Begin and reconciliation.
SELECT COALESCE((
  SELECT seq
  FROM audit_logs
  WHERE organization_id = @organization_id
    AND seq > @baseline
    AND seq <= @target
    AND action IN ('openrouter-key:disable', 'openrouter-key:enable')
    AND metadata->>'key_type' = @key_type::text
  ORDER BY seq DESC
  LIMIT 1
), 0)::bigint AS cursor;
