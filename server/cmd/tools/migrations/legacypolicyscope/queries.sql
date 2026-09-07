-- name: SetLocalTimeouts :one
SELECT
  set_config('lock_timeout', @lock_timeout::text, true) AS lock_timeout,
  set_config('statement_timeout', @statement_timeout::text, true) AS statement_timeout;

-- name: LockLegacyScopeBatch :many
SELECT
  p.id,
  p.action,
  p.policy_type,
  p.sources,
  p.custom_rule_ids,
  p.message_types,
  p.scope_include,
  p.scope_exempt,
  p.analyzer_config
FROM risk_policies AS p
WHERE p.deleted IS FALSE
  AND (
    (p.message_types IS NOT NULL AND cardinality(p.message_types) > 0)
    OR coalesce(p.scope_include, '') <> ''
    OR coalesce(p.scope_exempt, '') <> ''
  )
  AND p.id > @after_id::uuid
ORDER BY p.id
LIMIT @batch_size::int
FOR UPDATE OF p SKIP LOCKED;

-- name: ApplyFold :execrows
-- version identifies the scanning-relevant config a finding was produced under
-- (risk_results.risk_policy_version), so it bumps only when the fold actually
-- changed what the policy scans. A preserved fold is behaviour-identical and
-- must leave prior findings addressable.
UPDATE risk_policies
SET analyzer_config = @analyzer_config::jsonb,
    message_types = NULL,
    scope_include = NULL,
    scope_exempt = NULL,
    version = CASE WHEN @bump_version::boolean THEN version + 1 ELSE version END,
    updated_at = clock_timestamp()
WHERE id = @id::uuid
  AND deleted IS FALSE;

-- name: CountRemainingLegacyScopes :one
SELECT count(*) AS remaining
FROM risk_policies AS p
WHERE p.deleted IS FALSE
  AND (
    (p.message_types IS NOT NULL AND cardinality(p.message_types) > 0)
    OR coalesce(p.scope_include, '') <> ''
    OR coalesce(p.scope_exempt, '') <> ''
  );

-- name: CountLegacyScopesByAction :many
SELECT p.action, count(*) AS total
FROM risk_policies AS p
WHERE p.deleted IS FALSE
  AND (
    (p.message_types IS NOT NULL AND cardinality(p.message_types) > 0)
    OR coalesce(p.scope_include, '') <> ''
    OR coalesce(p.scope_exempt, '') <> ''
  )
GROUP BY p.action
ORDER BY p.action;
