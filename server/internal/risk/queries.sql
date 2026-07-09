-- name: CreateRiskPolicy :one
INSERT INTO risk_policies (
    id
  , project_id
  , organization_id
  , name
  , policy_type
  , sources
  , presidio_entities
  , analyzer_config
  , prompt_injection_rules
  , disabled_rules
  , custom_rule_ids
  , message_types
  , scope_include
  , scope_exempt
  , enabled
  , action
  , audience_type
  , auto_name
  , user_message
  , prompt
  , model_config
  , version
)
VALUES (
    @id
  , @project_id
  , @organization_id
  , @name
  , COALESCE(NULLIF(@policy_type, ''), 'standard')
  , @sources
  , @presidio_entities
  , COALESCE(sqlc.narg(analyzer_config)::jsonb, '{}'::jsonb)
  , @prompt_injection_rules
  , @disabled_rules
  , COALESCE(sqlc.arg(custom_rule_ids)::text[], '{}'::text[])
  , sqlc.arg(message_types)::text[]
  , sqlc.narg(scope_include)::text
  , sqlc.narg(scope_exempt)::text
  , @enabled
  , @action
  , @audience_type
  , @auto_name
  , @user_message
  , sqlc.narg(prompt)::text
  , sqlc.narg(model_config)::jsonb
  , 1
)
RETURNING *;

-- name: GetRiskPolicy :one
SELECT *
FROM risk_policies
WHERE id = @id
  AND project_id = @project_id
  AND deleted IS FALSE;

-- name: GetRiskPolicyForUpdate :one
SELECT *
FROM risk_policies
WHERE id = @id
  AND project_id = @project_id
  AND deleted IS FALSE
FOR UPDATE;

-- name: GetRiskPolicyNameIncludingDeleted :one
SELECT name
FROM risk_policies
WHERE id = @id
  AND project_id = @project_id;

-- name: ListRiskPolicies :many
SELECT *
FROM risk_policies
WHERE project_id = @project_id
  AND deleted IS FALSE
ORDER BY created_at DESC;

-- name: ListEnabledRiskPoliciesByProject :many
SELECT *
FROM risk_policies
WHERE project_id = @project_id
  AND enabled IS TRUE
  AND deleted IS FALSE;

-- name: UpdateRiskPolicy :one
UPDATE risk_policies
SET name = @name
  , sources = @sources
  , presidio_entities = @presidio_entities
  , analyzer_config = COALESCE(sqlc.narg(analyzer_config)::jsonb, '{}'::jsonb)
  , prompt_injection_rules = @prompt_injection_rules
  , disabled_rules = @disabled_rules
  , custom_rule_ids = COALESCE(sqlc.arg(custom_rule_ids)::text[], '{}'::text[])
  , message_types = sqlc.arg(message_types)::text[]
  , scope_include = sqlc.narg(scope_include)::text
  , scope_exempt = sqlc.narg(scope_exempt)::text
  , enabled = @enabled
  , action = @action
  , audience_type = @audience_type
  , auto_name = @auto_name
  , user_message = @user_message
  , prompt = sqlc.narg(prompt)::text
  , model_config = sqlc.narg(model_config)::jsonb
  , version = CASE
      WHEN sources IS DISTINCT FROM @sources
        OR presidio_entities IS DISTINCT FROM @presidio_entities
        OR analyzer_config IS DISTINCT FROM COALESCE(sqlc.narg(analyzer_config)::jsonb, '{}'::jsonb)
        OR prompt_injection_rules IS DISTINCT FROM @prompt_injection_rules
        OR disabled_rules IS DISTINCT FROM @disabled_rules
        OR custom_rule_ids IS DISTINCT FROM COALESCE(sqlc.arg(custom_rule_ids)::text[], '{}'::text[])
        OR message_types IS DISTINCT FROM sqlc.arg(message_types)::text[]
        OR scope_include IS DISTINCT FROM sqlc.narg(scope_include)::text
        OR scope_exempt IS DISTINCT FROM sqlc.narg(scope_exempt)::text
        OR enabled IS DISTINCT FROM @enabled
        OR action IS DISTINCT FROM @action
        OR prompt IS DISTINCT FROM sqlc.narg(prompt)::text
        OR model_config IS DISTINCT FROM sqlc.narg(model_config)::jsonb
        OR audience_type IS DISTINCT FROM @audience_type
      THEN version + 1
      ELSE version
    END
  , updated_at = clock_timestamp()
WHERE id = @id
  AND project_id = @project_id
  AND deleted IS FALSE
RETURNING *;

-- name: BumpRiskPolicyVersion :one
UPDATE risk_policies
SET version = version + 1
  , updated_at = clock_timestamp()
WHERE id = @id
  AND project_id = @project_id
  AND deleted IS FALSE
RETURNING *;

-- name: DeleteRiskPolicy :exec
UPDATE risk_policies
SET deleted_at = clock_timestamp()
  , updated_at = clock_timestamp()
WHERE id = @id
  AND project_id = @project_id
  AND deleted IS FALSE;

-- name: DeleteRiskPolicyBypassRequestsByPolicy :exec
UPDATE risk_policy_bypass_requests
SET deleted_at = clock_timestamp()
  , updated_at = clock_timestamp()
WHERE risk_policy_id = @risk_policy_id
  AND project_id = @project_id
  AND deleted IS FALSE;

-- name: DeleteRiskResultsByPolicy :execrows
DELETE FROM risk_results
WHERE risk_policy_id = @risk_policy_id
  AND project_id = @project_id;

-- name: DeleteRiskExclusionsByPolicy :execrows
DELETE FROM risk_exclusions
WHERE risk_policy_id = @risk_policy_id
  AND project_id = @project_id;

-- name: CountRiskResultsByPolicyID :one
SELECT COUNT(*)::BIGINT
FROM risk_results
WHERE risk_policy_id = @risk_policy_id
  AND project_id = @project_id;

-- name: UpsertRiskPolicyBypassRequest :one
INSERT INTO risk_policy_bypass_requests (
    id
  , organization_id
  , project_id
  , risk_policy_id
  , target_kind
  , target_label
  , target_key
  , target_dimensions
  , requester_user_id
  , requester_email
  , note
  , status
)
VALUES (
    @id
  , @organization_id
  , @project_id
  , @risk_policy_id
  , @target_kind
  , @target_label
  , @target_key
  , @target_dimensions
  , @requester_user_id
  , @requester_email
  , @note
  , @status
)
ON CONFLICT (project_id, requester_user_id, risk_policy_id, target_kind, target_key)
WHERE deleted IS FALSE
DO UPDATE
SET target_label = EXCLUDED.target_label
  , target_dimensions = EXCLUDED.target_dimensions
  , requester_email = EXCLUDED.requester_email
  , note = EXCLUDED.note
  , status = CASE
      WHEN risk_policy_bypass_requests.status = 'approved' THEN risk_policy_bypass_requests.status
      ELSE EXCLUDED.status
    END
  , decided_by = CASE
      WHEN risk_policy_bypass_requests.status = 'approved' THEN risk_policy_bypass_requests.decided_by
      ELSE NULL
    END
  , granted_principal_urns = CASE
      WHEN risk_policy_bypass_requests.status = 'approved' THEN risk_policy_bypass_requests.granted_principal_urns
      ELSE ARRAY[]::TEXT[]
    END
  , decided_at = CASE
      WHEN risk_policy_bypass_requests.status = 'approved' THEN risk_policy_bypass_requests.decided_at
      ELSE NULL
    END
  , updated_at = clock_timestamp()
RETURNING *;

-- name: UpsertRiskPolicyChallenge :one
-- Records (or refreshes) a warn/challenge for a (user, policy, tool). Never
-- stores the raw matched value. A live acknowledgement is preserved so a
-- re-scan does not wipe a still-valid ack before the agent retries.
INSERT INTO risk_policy_challenges (
    id
  , organization_id
  , project_id
  , risk_policy_id
  , user_id
  , tool_name
  , call_fingerprint
  , status
  , policy_name
  , entity
  , rule_id
  , challenged_at
)
VALUES (
    @id
  , @organization_id
  , @project_id
  , @risk_policy_id
  , @user_id
  , sqlc.narg(tool_name)::text
  , sqlc.narg(call_fingerprint)::text
  , 'challenged'
  , sqlc.narg(policy_name)::text
  , sqlc.narg(entity)::text
  , sqlc.narg(rule_id)::text
  , clock_timestamp()
)
ON CONFLICT (project_id, user_id, risk_policy_id, tool_name, call_fingerprint)
WHERE deleted IS FALSE
DO UPDATE
SET status = CASE
      WHEN risk_policy_challenges.status = 'acknowledged'
        AND risk_policy_challenges.expires_at IS NOT NULL
        AND risk_policy_challenges.expires_at > clock_timestamp()
      THEN risk_policy_challenges.status
      ELSE 'challenged'
    END
  , policy_name = EXCLUDED.policy_name
  , entity = EXCLUDED.entity
  , rule_id = EXCLUDED.rule_id
  , challenged_at = CASE
      WHEN risk_policy_challenges.status = 'acknowledged'
        AND risk_policy_challenges.expires_at IS NOT NULL
        AND risk_policy_challenges.expires_at > clock_timestamp()
      THEN risk_policy_challenges.challenged_at
      ELSE clock_timestamp()
    END
  , updated_at = clock_timestamp()
RETURNING *;

-- name: MarkRiskPolicyChallengeAcknowledged :one
-- Self-service redeem: mark the challenge acknowledged with a remember-until
-- expiry. Upserts so a redeem that beats the async challenge insert still works.
INSERT INTO risk_policy_challenges (
    id
  , organization_id
  , project_id
  , risk_policy_id
  , user_id
  , tool_name
  , call_fingerprint
  , status
  , policy_name
  , challenged_at
  , acknowledged_at
  , expires_at
)
VALUES (
    @id
  , @organization_id
  , @project_id
  , @risk_policy_id
  , @user_id
  , sqlc.narg(tool_name)::text
  , sqlc.narg(call_fingerprint)::text
  , 'acknowledged'
  , sqlc.narg(policy_name)::text
  , clock_timestamp()
  , clock_timestamp()
  , @expires_at
)
ON CONFLICT (project_id, user_id, risk_policy_id, tool_name, call_fingerprint)
WHERE deleted IS FALSE
DO UPDATE
SET status = 'acknowledged'
  , acknowledged_at = clock_timestamp()
  , expires_at = EXCLUDED.expires_at
  , policy_name = COALESCE(EXCLUDED.policy_name, risk_policy_challenges.policy_name)
  , updated_at = clock_timestamp()
-- A decline is final for this challenge: never resurrect a declined row into an
-- acknowledgement (e.g. a leaked/un-evicted token redeemed after decline). A
-- genuine re-challenge (UpsertRiskPolicyChallenge) resets declined -> challenged
-- first, after which acknowledgement can proceed. When this predicate is false
-- the update is a no-op and RETURNING yields no rows (ErrNoRows), which the
-- caller maps to a "declined" client error.
WHERE risk_policy_challenges.status <> 'declined'
RETURNING *;

-- name: MarkRiskPolicyChallengeDeclined :one
-- Self-service decline: mark the challenge declined and never grant an ack
-- window. Upserts so a decline that beats the async challenge insert still works.
INSERT INTO risk_policy_challenges (
    id
  , organization_id
  , project_id
  , risk_policy_id
  , user_id
  , tool_name
  , call_fingerprint
  , status
  , policy_name
  , challenged_at
)
VALUES (
    @id
  , @organization_id
  , @project_id
  , @risk_policy_id
  , @user_id
  , sqlc.narg(tool_name)::text
  , sqlc.narg(call_fingerprint)::text
  , 'declined'
  , sqlc.narg(policy_name)::text
  , clock_timestamp()
)
ON CONFLICT (project_id, user_id, risk_policy_id, tool_name, call_fingerprint)
WHERE deleted IS FALSE
DO UPDATE
SET status = 'declined'
  , acknowledged_at = NULL
  , expires_at = NULL
  , policy_name = COALESCE(EXCLUDED.policy_name, risk_policy_challenges.policy_name)
  , updated_at = clock_timestamp()
RETURNING *;

-- name: GetActiveRiskPolicyAck :one
-- Hook-time gate: is there a live acknowledgement for this concrete call
-- (user, policy, tool, call_fingerprint)? The agent retries with NO token, so
-- this table lookup — not the cache token — is what allows the retry through.
-- Scoped by call_fingerprint so only an identical retry clears, not any
-- same-tool call under the policy.
SELECT *
FROM risk_policy_challenges
WHERE project_id = @project_id
  AND user_id = @user_id
  AND risk_policy_id = @risk_policy_id
  AND tool_name IS NOT DISTINCT FROM sqlc.narg(tool_name)::text
  AND call_fingerprint IS NOT DISTINCT FROM sqlc.narg(call_fingerprint)::text
  AND status = 'acknowledged'
  AND expires_at IS NOT NULL
  AND expires_at > clock_timestamp()
  AND deleted IS FALSE;

-- name: ListRiskPolicyBypassRequests :many
SELECT *
FROM risk_policy_bypass_requests
WHERE project_id = @project_id
  AND deleted IS FALSE
  AND (
    sqlc.narg(risk_policy_id)::uuid IS NULL
    OR risk_policy_id = sqlc.narg(risk_policy_id)::uuid
  )
  AND (
    sqlc.narg(status)::text IS NULL
    OR status = sqlc.narg(status)::text
  )
ORDER BY updated_at DESC;

-- name: GetRiskPolicyBypassRequest :one
SELECT *
FROM risk_policy_bypass_requests
WHERE id = @id
  AND project_id = @project_id
  AND deleted IS FALSE;

-- name: UpdateRiskPolicyBypassRequestStatus :one
UPDATE risk_policy_bypass_requests
SET status = @status
  , decided_by = @decided_by
  , granted_principal_urns = @granted_principal_urns
  , decided_at = clock_timestamp()
  , updated_at = clock_timestamp()
WHERE id = @id
  AND project_id = @project_id
  AND deleted IS FALSE
RETURNING *;

-- name: CreateCustomDetectionRule :one
INSERT INTO risk_custom_detection_rules (
    project_id
  , organization_id
  , rule_id
  , title
  , description
  , detection_expr
  , severity
)
VALUES (
    @project_id
  , @organization_id
  , @rule_id
  , @title
  , @description
  , sqlc.narg(detection_expr)::text
  , @severity
)
RETURNING *;

-- name: ListCustomDetectionRules :many
SELECT *
FROM risk_custom_detection_rules
WHERE project_id = @project_id
  AND deleted IS FALSE
ORDER BY created_at DESC;

-- name: GetCustomDetectionRule :one
SELECT *
FROM risk_custom_detection_rules
WHERE id = @id
  AND project_id = @project_id
  AND deleted IS FALSE;

-- name: UpdateCustomDetectionRule :one
UPDATE risk_custom_detection_rules
SET title = @title
  , description = @description
  , detection_expr = sqlc.narg(detection_expr)::text
  , severity = @severity
  , updated_at = clock_timestamp()
WHERE id = @id
  AND project_id = @project_id
  AND deleted IS FALSE
RETURNING *;

-- name: DeleteCustomDetectionRule :exec
UPDATE risk_custom_detection_rules
SET deleted_at = clock_timestamp()
  , updated_at = clock_timestamp()
WHERE id = @id
  AND project_id = @project_id
  AND deleted IS FALSE;

-- name: CountTotalMessages :one
SELECT COUNT(*)::BIGINT
FROM chat_messages cm
WHERE cm.project_id = @project_id;

-- name: CountAnalyzedMessages :one
SELECT COUNT(DISTINCT rr.chat_message_id)::BIGINT
FROM risk_results rr
WHERE rr.project_id = @project_id
  AND rr.risk_policy_id = @risk_policy_id
  AND rr.risk_policy_version = @risk_policy_version;

-- name: CountFindingsByPolicy :one
SELECT COUNT(*)::BIGINT
FROM risk_results
WHERE project_id = @project_id
  AND risk_policy_id = @risk_policy_id
  AND risk_policy_version = @risk_policy_version
  AND found IS TRUE
  AND excluded_at IS NULL
  AND false_positive_at IS NULL;

-- name: CountAllFindings :one
SELECT COUNT(*)::BIGINT
FROM risk_results rr
JOIN risk_policies rp ON rp.id = rr.risk_policy_id AND rp.deleted IS FALSE AND rp.enabled IS TRUE
WHERE rr.project_id = @project_id
  AND rr.found IS TRUE AND rr.excluded_at IS NULL AND rr.false_positive_at IS NULL;

-- name: CountRiskResultsByProjectAndPolicy :one
-- Matches the filter semantics of ListRiskResultsByProjectAndPolicy: a
-- disabled policy still counts its historical findings, only deleted policies
-- are excluded.
SELECT COUNT(*)::BIGINT
FROM risk_results rr
JOIN risk_policies rp ON rp.id = rr.risk_policy_id AND rp.deleted IS FALSE
WHERE rr.project_id = @project_id
  AND rr.risk_policy_id = @risk_policy_id
  AND rr.found IS TRUE AND rr.excluded_at IS NULL AND rr.false_positive_at IS NULL;

-- name: GetRiskOverviewCounts :one
SELECT
    COUNT(DISTINCT rr.chat_message_id)::BIGINT AS messages_scanned
  , (COUNT(*) FILTER (
      WHERE rr.found IS TRUE AND rr.excluded_at IS NULL AND rr.false_positive_at IS NULL
    ))::BIGINT AS findings
  , (COUNT(DISTINCT cm.chat_id) FILTER (
      WHERE rr.found IS TRUE AND rr.excluded_at IS NULL AND rr.false_positive_at IS NULL
        AND cm.chat_id IS NOT NULL
    ))::BIGINT AS flagged_sessions
  , (
      SELECT COUNT(*)::BIGINT
      FROM risk_policies active_rp
      WHERE active_rp.project_id = @project_id
        AND enabled IS TRUE
        AND deleted IS FALSE
    ) AS active_policies
FROM risk_results rr
JOIN chat_messages cm ON cm.id = rr.chat_message_id
WHERE rr.project_id = @project_id
  AND rr.created_at >= @from_time
  AND rr.created_at < @to_time;

-- name: ListRiskOverviewTopRules :many
-- Project-wide finding counts grouped by rule_id within a window.
SELECT
  COALESCE(rr.rule_id, '')::TEXT AS rule_id,
  rr.source,
  COUNT(*)::BIGINT AS findings
FROM risk_results rr
WHERE rr.project_id = @project_id
  AND rr.found IS TRUE AND rr.excluded_at IS NULL AND rr.false_positive_at IS NULL
  AND rr.created_at >= @from_time
  AND rr.created_at < @to_time
GROUP BY rr.rule_id, rr.source
ORDER BY findings DESC, rule_id ASC
LIMIT @row_limit;

-- name: ListRiskUserCategoryBreakdown :many
-- Per-category finding counts for a single external_user_id in a window.
-- The category CASE expression must stay in sync with the other ListRisk*
-- queries.
WITH user_findings AS (
  SELECT
    CASE
      WHEN rr.source = 'llm_judge' THEN 'prompt_policy'
      WHEN rr.source IN ('shadow_mcp', 'destructive_tool', 'cli_destructive', 'prompt_injection') THEN rr.source
      WHEN rr.rule_id LIKE 'secret.%' THEN 'secrets'
      WHEN rr.rule_id IN ('pii.credit_card', 'pii.iban_code', 'pii.us_bank_number', 'pii.crypto') THEN 'financial'
      WHEN rr.rule_id IN (
          'pii.us_ssn'
        , 'pii.us_passport'
        , 'pii.us_driver_license'
        , 'pii.us_itin'
        , 'pii.uk_nhs'
        , 'pii.uk_nino'
        , 'pii.uk_passport'
        , 'pii.es_nif'
        , 'pii.it_fiscal_code'
        , 'pii.au_tfn'
        , 'pii.in_pan'
        , 'pii.in_aadhaar'
        , 'pii.sg_nric_fin'
      ) THEN 'government_ids'
      WHEN rr.rule_id IN (
          'pii.medical_license'
        , 'pii.us_mbi'
        , 'pii.us_npi'
        , 'pii.medical_disease_disorder'
        , 'pii.medical_medication'
        , 'pii.medical_therapeutic_procedure'
        , 'pii.medical_clinical_event'
        , 'pii.medical_biological_attribute'
        , 'pii.medical_family_history'
      ) THEN 'healthcare'
      WHEN rr.rule_id IN (
          'pii.harmful_content_request'
        , 'pii.policy_violation'
        , 'pii.unauthorized_action'
        , 'pii.topic_boundary_violation'
      ) THEN 'off_policy'
      WHEN rr.rule_id LIKE 'pii.%' THEN 'pii'
      -- Scanner-source fallbacks: keep these LAST so any prefixed
      -- rule_id wins. Stay in sync with the Go classifier in
      -- internal/risk/categories.
      WHEN rr.source = 'gitleaks' THEN 'secrets'
      WHEN rr.source = 'presidio' THEN 'pii'
      ELSE 'custom'
    END AS category
  FROM risk_results rr
  JOIN chat_messages cm ON cm.id = rr.chat_message_id
  LEFT JOIN chats c ON c.id = cm.chat_id AND c.deleted IS FALSE
  WHERE rr.project_id = @project_id
    AND rr.found IS TRUE AND rr.excluded_at IS NULL AND rr.false_positive_at IS NULL
    AND rr.created_at >= @from_time
    AND rr.created_at < @to_time
    AND COALESCE(NULLIF(cm.external_user_id, ''), NULLIF(c.external_user_id, ''), '') = @external_user_id::text
)
SELECT category, COUNT(*)::BIGINT AS findings
FROM user_findings
GROUP BY category
ORDER BY findings DESC, category ASC;

-- name: ListRiskUserRuleBreakdown :many
-- Per-rule_id finding counts for a single external_user_id in a window.
SELECT
  COALESCE(rr.rule_id, '')::TEXT AS rule_id,
  rr.source,
  COUNT(*)::BIGINT AS findings
FROM risk_results rr
JOIN chat_messages cm ON cm.id = rr.chat_message_id
LEFT JOIN chats c ON c.id = cm.chat_id AND c.deleted IS FALSE
WHERE rr.project_id = @project_id
  AND rr.found IS TRUE AND rr.excluded_at IS NULL AND rr.false_positive_at IS NULL
  AND rr.created_at >= @from_time
  AND rr.created_at < @to_time
  AND COALESCE(NULLIF(cm.external_user_id, ''), NULLIF(c.external_user_id, ''), '') = @external_user_id::text
GROUP BY rr.rule_id, rr.source
ORDER BY findings DESC, rule_id ASC;

-- name: ListRiskRulesByCategory :many
-- Returns per-rule_id finding counts for a category within a window.
-- The CASE expression must stay in sync with ListRiskOverviewTimeSeriesFindings
-- and ListRiskResultsByProjectFound; all three classify rr.rule_id the same way.
WITH categorized AS (
  SELECT
    COALESCE(rr.rule_id, '')::TEXT AS rule_id,
    rr.source,
    CASE
      WHEN rr.source = 'llm_judge' THEN 'prompt_policy'
      WHEN rr.source IN ('shadow_mcp', 'destructive_tool', 'cli_destructive', 'prompt_injection') THEN rr.source
      WHEN rr.rule_id LIKE 'secret.%' THEN 'secrets'
      WHEN rr.rule_id IN ('pii.credit_card', 'pii.iban_code', 'pii.us_bank_number', 'pii.crypto') THEN 'financial'
      WHEN rr.rule_id IN (
          'pii.us_ssn'
        , 'pii.us_passport'
        , 'pii.us_driver_license'
        , 'pii.us_itin'
        , 'pii.uk_nhs'
        , 'pii.uk_nino'
        , 'pii.uk_passport'
        , 'pii.es_nif'
        , 'pii.it_fiscal_code'
        , 'pii.au_tfn'
        , 'pii.in_pan'
        , 'pii.in_aadhaar'
        , 'pii.sg_nric_fin'
      ) THEN 'government_ids'
      WHEN rr.rule_id IN (
          'pii.medical_license'
        , 'pii.us_mbi'
        , 'pii.us_npi'
        , 'pii.medical_disease_disorder'
        , 'pii.medical_medication'
        , 'pii.medical_therapeutic_procedure'
        , 'pii.medical_clinical_event'
        , 'pii.medical_biological_attribute'
        , 'pii.medical_family_history'
      ) THEN 'healthcare'
      WHEN rr.rule_id IN (
          'pii.harmful_content_request'
        , 'pii.policy_violation'
        , 'pii.unauthorized_action'
        , 'pii.topic_boundary_violation'
      ) THEN 'off_policy'
      WHEN rr.rule_id LIKE 'pii.%' THEN 'pii'
      -- Scanner-source fallbacks: keep these LAST so any prefixed
      -- rule_id wins. Stay in sync with the Go classifier in
      -- internal/risk/categories.
      WHEN rr.source = 'gitleaks' THEN 'secrets'
      WHEN rr.source = 'presidio' THEN 'pii'
      ELSE 'custom'
    END AS category
  FROM risk_results rr
  WHERE rr.project_id = @project_id
    AND rr.found IS TRUE AND rr.excluded_at IS NULL AND rr.false_positive_at IS NULL
    AND rr.created_at >= @from_time
    AND rr.created_at < @to_time
)
SELECT rule_id, source, COUNT(*)::BIGINT AS findings
FROM categorized
WHERE category = @category::text
GROUP BY rule_id, source
ORDER BY findings DESC, rule_id ASC;

-- name: ListRiskOverviewTopUsers :many
WITH user_findings AS (
  SELECT
    COALESCE(NULLIF(cm.external_user_id, ''), NULLIF(c.external_user_id, ''), '')::TEXT AS external_user_id,
    COALESCE(
      NULLIF(u.email, ''),
      CASE WHEN cm.external_user_id LIKE '%@%' THEN cm.external_user_id END,
      CASE WHEN c.external_user_id LIKE '%@%' THEN c.external_user_id END,
      'Unknown user'
    )::TEXT AS email
  FROM risk_results rr
  JOIN chat_messages cm ON cm.id = rr.chat_message_id
  LEFT JOIN chats c ON c.id = cm.chat_id AND c.deleted IS FALSE
  LEFT JOIN users u ON u.id = COALESCE(NULLIF(cm.user_id, ''), NULLIF(c.user_id, ''))
  WHERE rr.project_id = @project_id
    AND rr.found IS TRUE AND rr.excluded_at IS NULL AND rr.false_positive_at IS NULL
    AND rr.created_at >= @from_time
    AND rr.created_at < @to_time
)
SELECT external_user_id, email, COUNT(*)::BIGINT AS findings
FROM user_findings
GROUP BY external_user_id, email
ORDER BY findings DESC, email ASC
LIMIT @row_limit;

-- name: ListRiskOverviewTimeSeriesFindings :many
WITH buckets AS (
  SELECT generate_series(
      date_trunc('hour', sqlc.arg(from_time)::timestamptz)
    , date_trunc('hour', (sqlc.arg(to_time)::timestamptz - INTERVAL '1 microsecond'))
    , INTERVAL '1 hour'
  )::timestamptz AS bucket_start
),
categorized AS (
  SELECT
      date_trunc('hour', rr.created_at)::timestamptz AS bucket_start
    , CASE
        WHEN rr.source = 'llm_judge' THEN 'prompt_policy'
        WHEN rr.source IN ('shadow_mcp', 'destructive_tool', 'cli_destructive', 'prompt_injection') THEN rr.source
        WHEN rr.rule_id LIKE 'secret.%' THEN 'secrets'
        WHEN rr.rule_id IN ('pii.credit_card', 'pii.iban_code', 'pii.us_bank_number', 'pii.crypto') THEN 'financial'
        WHEN rr.rule_id IN (
            'pii.us_ssn'
          , 'pii.us_passport'
          , 'pii.us_driver_license'
          , 'pii.us_itin'
          , 'pii.uk_nhs'
          , 'pii.uk_nino'
          , 'pii.uk_passport'
          , 'pii.es_nif'
          , 'pii.it_fiscal_code'
          , 'pii.au_tfn'
          , 'pii.in_pan'
          , 'pii.in_aadhaar'
          , 'pii.sg_nric_fin'
        ) THEN 'government_ids'
        WHEN rr.rule_id IN (
            'pii.medical_license'
          , 'pii.us_mbi'
          , 'pii.us_npi'
          , 'pii.medical_disease_disorder'
          , 'pii.medical_medication'
          , 'pii.medical_therapeutic_procedure'
          , 'pii.medical_clinical_event'
          , 'pii.medical_biological_attribute'
          , 'pii.medical_family_history'
        ) THEN 'healthcare'
        WHEN rr.rule_id IN (
            'pii.harmful_content_request'
          , 'pii.policy_violation'
          , 'pii.unauthorized_action'
          , 'pii.topic_boundary_violation'
        ) THEN 'off_policy'
        WHEN rr.rule_id LIKE 'pii.%' THEN 'pii'
        -- Scanner-source fallbacks: keep these LAST so any prefixed
        -- rule_id wins. Stay in sync with the Go classifier in
        -- internal/risk/categories.
        WHEN rr.source = 'gitleaks' THEN 'secrets'
        WHEN rr.source = 'presidio' THEN 'pii'
        ELSE 'custom'
      END AS category
  FROM risk_results rr
  WHERE rr.project_id = sqlc.arg(project_id)::uuid
    AND rr.found IS TRUE AND rr.excluded_at IS NULL AND rr.false_positive_at IS NULL
    AND rr.created_at >= @from_time
    AND rr.created_at < @to_time
),
categories AS (
  SELECT DISTINCT category
  FROM categorized
),
findings_by_bucket AS (
  SELECT
      bucket_start
    , category
    , COUNT(*)::BIGINT AS findings
  FROM categorized
  GROUP BY bucket_start, category
)
SELECT
    buckets.bucket_start
  , categories.category
  , COALESCE(findings_by_bucket.findings, 0)::BIGINT AS findings
FROM buckets
CROSS JOIN categories
LEFT JOIN findings_by_bucket ON findings_by_bucket.bucket_start = buckets.bucket_start AND findings_by_bucket.category = categories.category
ORDER BY buckets.bucket_start ASC, categories.category ASC;

-- name: FetchUnanalyzedMessageIDs :many
-- Scans the partial index chat_messages_risk_analyzed_at_null_idx
-- (project_id, id WHERE risk_analyzed_at IS NULL), which shrinks toward
-- zero at steady state. The id >= @id_lower_bound bound (a UUIDv7 lower
-- bound computed from the configured lookback) further limits the scan to
-- recent messages, reusing the same partial index ordering.
SELECT cm.id
FROM chat_messages cm
WHERE cm.project_id = @project_id
  AND cm.risk_analyzed_at IS NULL
  AND cm.id >= @id_lower_bound
ORDER BY cm.id DESC
LIMIT @batch_limit;

-- name: MarkMessagesRiskAnalyzed :exec
UPDATE chat_messages
SET risk_analyzed_at = clock_timestamp()
WHERE id = ANY(@message_ids::uuid[])
  AND project_id = @project_id;

-- name: GetMessageContentBatch :many
SELECT id, role, content, tool_calls
FROM chat_messages
WHERE id = ANY(@ids::uuid[])
  AND project_id = @project_id;

-- name: GetBatchChatIdentities :many
-- One row per chat represented in a batch of messages, for the session-scoped
-- account_identity scanner: the chat's earliest in-batch message (UUIDv7
-- order = creation order, the message the finding attaches to) plus the
-- chat's AI-account identity from personal-account tracking (NULL
-- account_type/email for unattributed chats — the scanner emits nothing for
-- those).
--
-- flagged_rule_ids carries the account_identity rules already recorded for
-- the chat at this policy version on messages OUTSIDE the batch; the scanner
-- drops those rules and emits only the rest, so session-scoped findings
-- dedupe to one per chat PER RULE per policy version. Dedupe must be
-- rule-scoped, not chat-scoped: identity fields arrive incrementally (an
-- account can be classified personal before its email is known, and vice
-- versa), so a later batch can legitimately fire a rule the first batch could
-- not evaluate yet. Findings on in-batch messages do not block re-emission
-- because the writer deletes and re-inserts results for the batch's messages.
SELECT earliest_message_id, chat_id, account_type, email, flagged_rule_ids
FROM (
  SELECT DISTINCT ON (cm.chat_id)
      cm.id AS earliest_message_id
    , cm.chat_id
    , ua.account_type
    , ua.email
    , (
        SELECT COALESCE(array_agg(DISTINCT rr.rule_id), '{}')
        FROM risk_results rr
        JOIN chat_messages prior ON prior.id = rr.chat_message_id
        WHERE rr.project_id = @project_id::uuid
          AND rr.risk_policy_id = @risk_policy_id
          AND rr.risk_policy_version = @risk_policy_version
          AND rr.source = 'account_identity'
          AND rr.found IS TRUE
          AND rr.rule_id IS NOT NULL
          AND prior.chat_id = cm.chat_id
          AND rr.chat_message_id != ALL(@ids::uuid[])
      )::text[] AS flagged_rule_ids
  FROM chat_messages cm
  JOIN chats c ON c.id = cm.chat_id AND c.deleted IS FALSE
  LEFT JOIN user_accounts ua ON ua.id = c.user_account_id AND ua.deleted IS FALSE
  WHERE cm.id = ANY(@ids::uuid[])
    AND cm.project_id = @project_id::uuid
  ORDER BY cm.chat_id, cm.id ASC
) batch_chats
ORDER BY earliest_message_id ASC;

-- name: RefreshAccountIdentityFindingMatch :execrows
-- Enriches an already-recorded account_identity finding in place once identity
-- fields that were unknown at first analysis arrive — e.g. a personal account
-- classified before its email was known. Session-scoped findings dedupe per
-- rule (GetBatchChatIdentities.flagged_rule_ids), so a later batch's richer
-- finding is dropped rather than inserted; without this the original row keeps
-- its empty match and generic description forever. Restricted to rows whose
-- match is still empty so it is idempotent and never clobbers a finding that
-- already carries its detail.
UPDATE risk_results rr
SET description = @description, match = @match
FROM chat_messages cm
WHERE rr.chat_message_id = cm.id
  AND cm.chat_id = @chat_id::uuid
  AND rr.project_id = @project_id::uuid
  AND rr.risk_policy_id = @risk_policy_id
  AND rr.risk_policy_version = @risk_policy_version
  AND rr.source = 'account_identity'
  AND rr.rule_id = @rule_id
  AND rr.found IS TRUE
  AND (rr.match IS NULL OR rr.match = '');

-- name: InsertRiskResults :copyfrom
INSERT INTO risk_results (
    id
  , project_id
  , organization_id
  , risk_policy_id
  , risk_policy_version
  , chat_message_id
  , source
  , found
  , rule_id
  , description
  , match
  , start_pos
  , end_pos
  , confidence
  , tags
  , spans
  , dead_letter_reason
)
VALUES (
    @id
  , @project_id
  , @organization_id
  , @risk_policy_id
  , @risk_policy_version
  , @chat_message_id
  , @source
  , @found
  , @rule_id
  , @description
  , @match
  , @start_pos
  , @end_pos
  , @confidence
  , @tags
  , @spans
  , @dead_letter_reason
);

-- name: DeleteRiskResultsForMessages :exec
DELETE FROM risk_results
WHERE risk_policy_id = @risk_policy_id
  AND project_id = @project_id
  AND chat_message_id = ANY(@message_ids::uuid[]);

-- name: GetRiskResultByID :one
-- Single-row lookup backing risk.results.unmask: fetch a result's raw match
-- plus its owning chat_id so the caller can authorize a chat:read check
-- before returning the plaintext. Applies the same found/excluded/
-- false-positive filters as every other risk_results read so a stale result
-- id (excluded or swept as noise since it was listed) can't be unmasked.
SELECT rr.id, rr.match, rr.source, cm.chat_id
FROM risk_results rr
JOIN chat_messages cm ON cm.id = rr.chat_message_id
WHERE rr.id = @id
  AND rr.project_id = @project_id
  AND rr.found IS TRUE
  AND rr.excluded_at IS NULL
  AND rr.false_positive_at IS NULL;

-- name: ListRiskResultsByProjectFound :many
-- Sort by the underlying chat message's created_at (the event time), NOT
-- rr.created_at (the scan time). The background drain workflow analyzes
-- historical messages in arbitrary order, so rr.created_at can put a
-- finding for an old message ahead of one for a recent message — which is
-- exactly the "random-seeming" order users see in Recent Findings.
-- Cursor is (cm.created_at, rr.id) for stable pagination.
-- The category CASE expression here must stay in sync with the one in
-- ListRiskOverviewTimeSeriesFindings; both derive the user-facing category
-- key from rr.source and rr.rule_id.
--
-- When @unique_match is TRUE, dedup at the SQL layer: keep only one row per
-- (risk_policy_id, rule_id, match), choosing the most recent occurrence. Done
-- inside a subquery so pagination over the deduped stream stays correct
-- (client-side dedup over paged data broke "Load more").
--
-- @policy_id is optional. When NULL only enabled policies are considered (the
-- default project-wide view). When set the results are scoped to that policy
-- AND disabled-but-not-deleted policies are included, so explicitly filtering
-- to a policy still surfaces its historical findings after it was turned off.
SELECT
    sub.id, sub.project_id, sub.organization_id, sub.risk_policy_id,
    sub.risk_policy_version, sub.chat_message_id, sub.source, sub.found,
    sub.rule_id, sub.description, sub.match, sub.start_pos, sub.end_pos,
    sub.confidence, sub.tags, sub.spans, sub.dead_letter_reason, sub.created_at,
    sub.chat_id, sub.message_created_at, sub.chat_title, sub.chat_user_id,
    sub.block_id
FROM (
  SELECT
      rr.id, rr.project_id, rr.organization_id, rr.risk_policy_id,
      rr.risk_policy_version, rr.chat_message_id, rr.source, rr.found,
      rr.rule_id, rr.description, rr.match, rr.start_pos, rr.end_pos,
      rr.confidence, rr.tags, rr.spans, rr.dead_letter_reason, rr.created_at,
      cm.chat_id, cm.created_at AS message_created_at,
      c.title AS chat_title, c.external_user_id AS chat_user_id,
      COALESCE(blk.block_id, '00000000-0000-0000-0000-000000000000'::uuid) AS block_id,
      CASE
        WHEN @unique_match::boolean THEN ROW_NUMBER() OVER (
          PARTITION BY rr.risk_policy_id, rr.rule_id, rr.match
          ORDER BY cm.created_at DESC, rr.id DESC
        )
        ELSE 1
      END AS dedup_rank
  FROM risk_results rr
  JOIN chat_messages cm ON cm.id = rr.chat_message_id
  LEFT JOIN chats c ON c.id = cm.chat_id AND c.deleted IS FALSE
  JOIN risk_policies rp ON rp.id = rr.risk_policy_id AND rp.deleted IS FALSE
    AND (rp.enabled IS TRUE OR rr.risk_policy_id = sqlc.narg(policy_id)::uuid)
  LEFT JOIN LATERAL (
    SELECT tcb.id AS block_id FROM tool_call_blocks tcb
    WHERE tcb.project_id = rr.project_id
      AND tcb.chat_message_id = rr.chat_message_id
      AND tcb.deleted IS FALSE
    ORDER BY tcb.created_at DESC LIMIT 1
  ) blk ON TRUE
  WHERE rr.project_id = @project_id
    AND rr.found IS TRUE AND rr.excluded_at IS NULL AND rr.false_positive_at IS NULL
    AND (sqlc.narg(policy_id)::uuid IS NULL OR rr.risk_policy_id = sqlc.narg(policy_id)::uuid)
    AND (sqlc.narg(from_time)::timestamptz IS NULL OR cm.created_at >= sqlc.narg(from_time)::timestamptz)
    AND (sqlc.narg(to_time)::timestamptz IS NULL OR cm.created_at < sqlc.narg(to_time)::timestamptz)
    AND (@rule_id::text = '' OR rr.rule_id ILIKE '%' || @rule_id::text || '%')
    AND (@user_id::text = '' OR c.external_user_id ILIKE '%' || @user_id::text || '%')
    AND (@category::text = '' OR (
    CASE
      WHEN rr.source = 'llm_judge' THEN 'prompt_policy'
      WHEN rr.source IN ('shadow_mcp', 'destructive_tool', 'cli_destructive', 'prompt_injection') THEN rr.source
      WHEN rr.rule_id LIKE 'secret.%' THEN 'secrets'
      WHEN rr.rule_id IN ('pii.credit_card', 'pii.iban_code', 'pii.us_bank_number', 'pii.crypto') THEN 'financial'
      WHEN rr.rule_id IN (
          'pii.us_ssn'
        , 'pii.us_passport'
        , 'pii.us_driver_license'
        , 'pii.us_itin'
        , 'pii.uk_nhs'
        , 'pii.uk_nino'
        , 'pii.uk_passport'
        , 'pii.es_nif'
        , 'pii.it_fiscal_code'
        , 'pii.au_tfn'
        , 'pii.in_pan'
        , 'pii.in_aadhaar'
        , 'pii.sg_nric_fin'
      ) THEN 'government_ids'
      WHEN rr.rule_id IN (
          'pii.medical_license'
        , 'pii.us_mbi'
        , 'pii.us_npi'
        , 'pii.medical_disease_disorder'
        , 'pii.medical_medication'
        , 'pii.medical_therapeutic_procedure'
        , 'pii.medical_clinical_event'
        , 'pii.medical_biological_attribute'
        , 'pii.medical_family_history'
      ) THEN 'healthcare'
      WHEN rr.rule_id IN (
          'pii.harmful_content_request'
        , 'pii.policy_violation'
        , 'pii.unauthorized_action'
        , 'pii.topic_boundary_violation'
      ) THEN 'off_policy'
      WHEN rr.rule_id LIKE 'pii.%' THEN 'pii'
      -- Scanner-source fallbacks: keep these LAST so any prefixed
      -- rule_id wins. Stay in sync with the Go classifier in
      -- internal/risk/categories.
      WHEN rr.source = 'gitleaks' THEN 'secrets'
      WHEN rr.source = 'presidio' THEN 'pii'
      ELSE 'custom'
    END
  ) = @category::text)
) sub
WHERE sub.dedup_rank = 1
  AND (
    sqlc.narg(cursor_message_created_at)::timestamptz IS NULL
    OR (sub.message_created_at, sub.id) < (sqlc.narg(cursor_message_created_at)::timestamptz, sqlc.narg(cursor_id)::uuid)
  )
ORDER BY sub.message_created_at DESC, sub.id DESC
LIMIT @page_limit;

-- name: ListRiskResultsByProjectAndPolicy :many
-- Unlike the other list queries, this does NOT require the policy to be
-- enabled. When the user explicitly filters to a specific policy we surface its
-- historical findings even after it has been turned off, so disabled policies
-- still show the matches they produced while active. Deleted policies remain
-- excluded. The frontend flags the inactive policy as historical data.
SELECT rr.*, cm.chat_id, cm.created_at AS message_created_at, c.title AS chat_title, c.external_user_id AS chat_user_id, COALESCE(blk.block_id, '00000000-0000-0000-0000-000000000000'::uuid) AS block_id
FROM risk_results rr
JOIN chat_messages cm ON cm.id = rr.chat_message_id
LEFT JOIN chats c ON c.id = cm.chat_id AND c.deleted IS FALSE
JOIN risk_policies rp ON rp.id = rr.risk_policy_id AND rp.deleted IS FALSE
LEFT JOIN LATERAL (
  SELECT tcb.id AS block_id FROM tool_call_blocks tcb
  WHERE tcb.project_id = rr.project_id
    AND tcb.chat_message_id = rr.chat_message_id
    AND tcb.deleted IS FALSE
  ORDER BY tcb.created_at DESC LIMIT 1
) blk ON TRUE
WHERE rr.project_id = @project_id
  AND rr.risk_policy_id = @risk_policy_id
  AND rr.found IS TRUE AND rr.excluded_at IS NULL AND rr.false_positive_at IS NULL
  AND (
    sqlc.narg(cursor_message_created_at)::timestamptz IS NULL
    OR (cm.created_at, rr.id) < (sqlc.narg(cursor_message_created_at)::timestamptz, sqlc.narg(cursor_id)::uuid)
  )
ORDER BY cm.created_at DESC, rr.id DESC
LIMIT @page_limit;

-- name: ListRiskResultsByChatFound :many
SELECT rr.*, cm.chat_id, cm.created_at AS message_created_at, c.title AS chat_title, c.external_user_id AS chat_user_id, COALESCE(blk.block_id, '00000000-0000-0000-0000-000000000000'::uuid) AS block_id
FROM risk_results rr
JOIN chat_messages cm ON cm.id = rr.chat_message_id
LEFT JOIN chats c ON c.id = cm.chat_id AND c.deleted IS FALSE
JOIN risk_policies rp ON rp.id = rr.risk_policy_id AND rp.deleted IS FALSE AND rp.enabled IS TRUE
LEFT JOIN LATERAL (
  SELECT tcb.id AS block_id FROM tool_call_blocks tcb
  WHERE tcb.project_id = rr.project_id
    AND tcb.chat_message_id = rr.chat_message_id
    AND tcb.deleted IS FALSE
  ORDER BY tcb.created_at DESC LIMIT 1
) blk ON TRUE
WHERE cm.chat_id = @chat_id
  AND rr.project_id = @project_id
  AND rr.found IS TRUE AND rr.excluded_at IS NULL AND rr.false_positive_at IS NULL
  AND (
    sqlc.narg(cursor_message_created_at)::timestamptz IS NULL
    OR (cm.created_at, rr.id) < (sqlc.narg(cursor_message_created_at)::timestamptz, sqlc.narg(cursor_id)::uuid)
  )
ORDER BY cm.created_at DESC, rr.id DESC
LIMIT @page_limit;

-- name: ListRiskResultsGroupedByChat :many
SELECT
    cm.chat_id
  , c.title AS chat_title
  , c.external_user_id AS chat_user_id
  , COUNT(*)::BIGINT AS findings_count
  , MAX(rr.created_at)::TIMESTAMPTZ AS latest_detected
FROM risk_results rr
JOIN chat_messages cm ON cm.id = rr.chat_message_id
LEFT JOIN chats c ON c.id = cm.chat_id AND c.deleted IS FALSE
JOIN risk_policies rp ON rp.id = rr.risk_policy_id AND rp.deleted IS FALSE AND rp.enabled IS TRUE
WHERE rr.project_id = @project_id
  AND rr.found IS TRUE AND rr.excluded_at IS NULL AND rr.false_positive_at IS NULL
  AND (sqlc.narg(cursor)::uuid IS NULL OR cm.chat_id <= sqlc.narg(cursor)::uuid)
GROUP BY cm.chat_id, c.title, c.external_user_id
ORDER BY cm.chat_id DESC
LIMIT @page_limit;

-- name: ListEnabledEnforcingPoliciesByProject :many
-- Enforcing actions are block (hard deny) and warn (challenge: deny + ack link,
-- allowed after acknowledgement). flag is non-enforcing and excluded.
SELECT *
FROM risk_policies
WHERE project_id = @project_id
  AND enabled IS TRUE
  AND action IN ('block', 'warn')
  AND deleted IS FALSE;

-- name: GetProjectFlagGroups :one
-- Resolves the org and project slugs used to build PostHog flag-evaluation
-- groups for background paths that only carry IDs. Both joins are on
-- primary/unique keys, so this is a cheap indexed lookup.
SELECT om.slug AS organization_slug, p.slug AS project_slug
FROM projects p
JOIN organization_metadata om ON om.id = p.organization_id
WHERE p.id = @project_id
  AND p.deleted IS FALSE;

-- name: ListEnabledShadowMCPPoliciesByProject :many
SELECT *
FROM risk_policies
WHERE project_id = @project_id
  AND enabled IS TRUE
  AND deleted IS FALSE
  AND 'shadow_mcp' = ANY(sources)
ORDER BY id;

-- name: ListEnabledToolIdentityPoliciesByProject :many
SELECT *
FROM risk_policies
WHERE project_id = @project_id
  AND enabled IS TRUE
  AND deleted IS FALSE
  AND (
    'shadow_mcp' = ANY(sources)
    OR 'destructive_tool' = ANY(sources)
  )
ORDER BY id;

-- name: HardDeleteRiskPoliciesByProject :exec
-- Test-only helper: hard-deletes every risk policy for a project so tests can
-- verify cache behavior without the soft-delete (DeleteRiskPolicy) leaving
-- ghost rows that production lookups already filter out.
DELETE FROM risk_policies WHERE project_id = @project_id;

-- Risk exclusions ----------------------------------------------------------
-- risk_policy_id is nullable: NULL = global (applies to every policy in the
-- project), non-NULL = bound to a single policy.

-- name: CreateRiskExclusion :one
INSERT INTO risk_exclusions (
    project_id
  , organization_id
  , risk_policy_id
  , match_type
  , match_value
  , rule_id_filter
  , source_filter
  , enabled
)
VALUES (
    @project_id
  , @organization_id
  , sqlc.narg(risk_policy_id)
  , @match_type
  , @match_value
  , @rule_id_filter
  , @source_filter
  , @enabled
)
RETURNING *;

-- name: GetRiskExclusion :one
SELECT *
FROM risk_exclusions
WHERE id = @id
  AND project_id = @project_id
  AND deleted IS FALSE;

-- name: GetRiskExclusionForReconcile :one
-- Fetches an exclusion regardless of deleted/enabled state so the reconcile
-- sweep can decide whether to apply (enabled) or only reverse (deleted/disabled).
-- Scoped by project_id to keep the IDOR-mitigation invariant (every query is
-- bounded to a tenant) even though the caller is an internal activity.
SELECT *
FROM risk_exclusions
WHERE id = @id
  AND project_id = @project_id;

-- name: ListRiskExclusionsByProject :many
-- Lists a project's exclusions. Pass a null risk_policy_id to return all
-- (global + every policy); pass a value to filter to that policy only.
SELECT *
FROM risk_exclusions
WHERE project_id = @project_id
  AND deleted IS FALSE
  AND (sqlc.narg(risk_policy_id)::uuid IS NULL OR risk_policy_id = sqlc.narg(risk_policy_id))
ORDER BY created_at DESC;

-- name: ListEnabledExclusionsForPolicy :many
-- Exclusions that apply when analyzing/enforcing a given policy: the policy's
-- own plus every global one. Used to build the going-forward ExclusionSet.
SELECT *
FROM risk_exclusions
WHERE project_id = @project_id
  AND enabled IS TRUE
  AND deleted IS FALSE
  AND (risk_policy_id IS NULL OR risk_policy_id = @risk_policy_id)
ORDER BY created_at;

-- name: CountEnabledRegexExclusionsInScope :one
-- Enforces the per-scope regex cap. Counts enabled regex exclusions sharing the
-- same scope (same risk_policy_id, treating NULL/global as its own bucket).
SELECT COUNT(*)::BIGINT
FROM risk_exclusions
WHERE project_id = @project_id
  AND match_type = 'regex'
  AND enabled IS TRUE
  AND deleted IS FALSE
  AND risk_policy_id IS NOT DISTINCT FROM sqlc.narg(risk_policy_id);

-- name: UpdateRiskExclusion :one
UPDATE risk_exclusions
SET risk_policy_id = sqlc.narg(risk_policy_id)
  , match_type = @match_type
  , match_value = @match_value
  , rule_id_filter = @rule_id_filter
  , source_filter = @source_filter
  , enabled = @enabled
  , updated_at = clock_timestamp()
WHERE id = @id
  AND project_id = @project_id
  AND deleted IS FALSE
RETURNING *;

-- name: DeleteRiskExclusion :exec
UPDATE risk_exclusions
SET deleted_at = clock_timestamp()
  , updated_at = clock_timestamp()
WHERE id = @id
  AND project_id = @project_id
  AND deleted IS FALSE;

-- Exclusion reconcile sweep -------------------------------------------------
-- All batches are keyset-paginated by id (id > @cursor, ORDER BY id, LIMIT
-- @batch_limit) and RETURNING id so the caller can advance the cursor to the
-- max returned id and loop until a batch comes back short.

-- name: ReverseExclusionFlagsBatch :many
-- Clears flags previously set by an exclusion (reversal / restore findings).
UPDATE risk_results
SET excluded_at = NULL
  , excluded_exclusion_id = NULL
WHERE id IN (
  SELECT rr.id
  FROM risk_results rr
  WHERE rr.excluded_exclusion_id = @exclusion_id
    AND rr.id > @cursor
  ORDER BY rr.id
  LIMIT @batch_limit
)
RETURNING id;

-- name: ApplyExactExclusionBatch :many
UPDATE risk_results
SET excluded_at = clock_timestamp()
  , excluded_exclusion_id = @exclusion_id
WHERE id IN (
  SELECT rr.id
  FROM risk_results rr
  WHERE rr.project_id = @project_id
    AND (sqlc.narg(policy_id)::uuid IS NULL OR rr.risk_policy_id = sqlc.narg(policy_id))
    AND rr.found IS TRUE
    AND rr.excluded_at IS NULL
    AND rr.match = @match_value
    AND (sqlc.narg(rule_id_filter)::text IS NULL OR rr.rule_id = sqlc.narg(rule_id_filter))
    AND (sqlc.narg(source_filter)::text IS NULL OR rr.source = sqlc.narg(source_filter))
    AND rr.id > @cursor
  ORDER BY rr.id
  LIMIT @batch_limit
)
RETURNING id;

-- name: ApplyRegexExclusionBatch :many
UPDATE risk_results
SET excluded_at = clock_timestamp()
  , excluded_exclusion_id = @exclusion_id
WHERE id IN (
  SELECT rr.id
  FROM risk_results rr
  WHERE rr.project_id = @project_id
    AND (sqlc.narg(policy_id)::uuid IS NULL OR rr.risk_policy_id = sqlc.narg(policy_id))
    AND rr.found IS TRUE
    AND rr.excluded_at IS NULL
    AND rr.match ~ @pattern
    AND (sqlc.narg(rule_id_filter)::text IS NULL OR rr.rule_id = sqlc.narg(rule_id_filter))
    AND (sqlc.narg(source_filter)::text IS NULL OR rr.source = sqlc.narg(source_filter))
    AND rr.id > @cursor
  ORDER BY rr.id
  LIMIT @batch_limit
)
RETURNING id;

-- name: ApplyRuleIDExclusionBatch :many
UPDATE risk_results
SET excluded_at = clock_timestamp()
  , excluded_exclusion_id = @exclusion_id
WHERE id IN (
  SELECT rr.id
  FROM risk_results rr
  WHERE rr.project_id = @project_id
    AND (sqlc.narg(policy_id)::uuid IS NULL OR rr.risk_policy_id = sqlc.narg(policy_id))
    AND rr.found IS TRUE
    AND rr.excluded_at IS NULL
    AND rr.rule_id = @match_value
    AND (sqlc.narg(source_filter)::text IS NULL OR rr.source = sqlc.narg(source_filter))
    AND rr.id > @cursor
  ORDER BY rr.id
  LIMIT @batch_limit
)
RETURNING id;

-- name: ApplySourceExclusionBatch :many
UPDATE risk_results
SET excluded_at = clock_timestamp()
  , excluded_exclusion_id = @exclusion_id
WHERE id IN (
  SELECT rr.id
  FROM risk_results rr
  WHERE rr.project_id = @project_id
    AND (sqlc.narg(policy_id)::uuid IS NULL OR rr.risk_policy_id = sqlc.narg(policy_id))
    AND rr.found IS TRUE
    AND rr.excluded_at IS NULL
    AND rr.source = @match_value
    AND (sqlc.narg(rule_id_filter)::text IS NULL OR rr.rule_id = sqlc.narg(rule_id_filter))
    AND rr.id > @cursor
  ORDER BY rr.id
  LIMIT @batch_limit
)
RETURNING id;

-- name: GetToolCallBlock :one
-- Loads a durable tool call block for the block page. Scoped to the viewer's
-- organization MEMBERSHIP (not their active org): the block is opened from the
-- link an agent embeds in its block message, often by a non-admin member, so it
-- is readable by any active member of the organization that owns it. The policy
-- name is joined for display when the policy still exists.
--
-- user_id is the owning user captured at deny time (empty string when it could
-- not be resolved). The handler uses it to tighten access beyond bare
-- membership: the owner may always view their block, anyone else needs
-- project-admin, and an empty user_id falls back to membership.
SELECT
    b.id,
    b.project_id,
    b.reason,
    b.tool_name,
    b.feedback,
    b.created_at,
    b.user_id,
    rp.name AS policy_name
FROM tool_call_blocks b
LEFT JOIN risk_policies rp ON rp.id = b.risk_policy_id AND rp.deleted IS FALSE
WHERE b.id = @id
  AND b.deleted IS FALSE
  AND EXISTS (
    SELECT 1
    FROM users u
    JOIN organization_user_relationships our ON our.user_id = u.id
    WHERE u.id = @viewer_user_id
      AND u.deleted_at IS NULL
      AND our.organization_id = b.organization_id
      AND our.deleted_at IS NULL
  );

-- name: UpdateToolCallBlockFeedback :one
-- Records the latest thumbs feedback on a block and returns the refreshed row.
-- Scoped to org membership (see GetToolCallBlock): only an active member of the
-- block's owning organization can vote.
UPDATE tool_call_blocks
SET feedback = sqlc.arg(feedback),
    feedback_user_id = sqlc.narg(feedback_user_id),
    feedback_at = clock_timestamp(),
    updated_at = clock_timestamp()
WHERE tool_call_blocks.id = sqlc.arg(id)
  AND tool_call_blocks.deleted IS FALSE
  AND EXISTS (
    SELECT 1
    FROM users u
    JOIN organization_user_relationships our ON our.user_id = u.id
    WHERE u.id = sqlc.arg(viewer_user_id)
      AND u.deleted_at IS NULL
      AND our.organization_id = tool_call_blocks.organization_id
      AND our.deleted_at IS NULL
  )
RETURNING tool_call_blocks.id, tool_call_blocks.project_id, tool_call_blocks.reason, tool_call_blocks.tool_name,
  tool_call_blocks.feedback, tool_call_blocks.created_at,
  COALESCE((SELECT rp.name FROM risk_policies rp WHERE rp.id = tool_call_blocks.risk_policy_id AND rp.deleted IS FALSE), '')::text AS policy_name;


-- name: UpsertRiskPolicyEvalReview :one
-- Records (or replaces) the current reviewer's ground-truth verdict for one
-- chat session under a prompt-based policy. Scoped to project_id. Upserts on the
-- active-row unique key so a reviewer has at most one verdict per session.
INSERT INTO risk_policy_eval_reviews (
  project_id, organization_id, risk_policy_id, risk_policy_version, chat_id, verdict, reviewed_by
) VALUES (
  @project_id, @organization_id, @risk_policy_id, @risk_policy_version, @chat_id, @verdict, @reviewed_by
)
ON CONFLICT (project_id, risk_policy_id, chat_id, reviewed_by) WHERE deleted IS FALSE
DO UPDATE SET
  verdict = EXCLUDED.verdict,
  risk_policy_version = EXCLUDED.risk_policy_version,
  updated_at = clock_timestamp()
RETURNING *;

-- name: RiskEvalChatBelongsToProject :one
SELECT EXISTS (
  SELECT 1
  FROM chats
  WHERE id = @chat_id
    AND project_id = @project_id
    AND deleted IS FALSE
);

-- name: ListRiskPolicyEvalReviews :many
-- The active regression set for a policy: every reviewer's current verdicts.
-- Scoped to project_id.
SELECT *
FROM risk_policy_eval_reviews
WHERE project_id = @project_id
  AND risk_policy_id = @risk_policy_id
  AND deleted IS FALSE
ORDER BY created_at DESC;

-- name: SoftDeleteRiskPolicyEvalReview :one
-- Removes the current reviewer's verdict for one session (the toggle-off path).
-- Scoped to project_id and reviewed_by so a reviewer only clears their own row.
UPDATE risk_policy_eval_reviews
SET deleted_at = clock_timestamp(),
    updated_at = clock_timestamp()
WHERE project_id = @project_id
  AND risk_policy_id = @risk_policy_id
  AND chat_id = @chat_id
  AND reviewed_by = @reviewed_by
  AND deleted IS FALSE
RETURNING *;

-- name: SetRiskResultExcludedForTest :exec
UPDATE risk_results
SET excluded_at = clock_timestamp()
WHERE id = @id;

-- name: SetRiskResultFalsePositiveForTest :exec
UPDATE risk_results
SET false_positive_at = clock_timestamp()
WHERE id = @id;
