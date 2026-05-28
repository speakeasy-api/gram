-- name: CreateRiskPolicy :one
INSERT INTO risk_policies (
    id
  , project_id
  , organization_id
  , name
  , sources
  , presidio_entities
  , prompt_injection_rules
  , disabled_rules
  , enabled
  , action
  , auto_name
  , user_message
  , version
)
VALUES (
    @id
  , @project_id
  , @organization_id
  , @name
  , @sources
  , @presidio_entities
  , @prompt_injection_rules
  , @disabled_rules
  , @enabled
  , @action
  , @auto_name
  , @user_message
  , 1
)
RETURNING *;

-- name: GetRiskPolicy :one
SELECT *
FROM risk_policies
WHERE id = @id
  AND project_id = @project_id
  AND deleted IS FALSE;

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
  , prompt_injection_rules = @prompt_injection_rules
  , disabled_rules = @disabled_rules
  , enabled = @enabled
  , action = @action
  , auto_name = @auto_name
  , user_message = @user_message
  , version = CASE
      WHEN sources IS DISTINCT FROM @sources
        OR presidio_entities IS DISTINCT FROM @presidio_entities
        OR prompt_injection_rules IS DISTINCT FROM @prompt_injection_rules
        OR disabled_rules IS DISTINCT FROM @disabled_rules
        OR enabled IS DISTINCT FROM @enabled
        OR action IS DISTINCT FROM @action
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
  AND found IS TRUE;

-- name: CountAllFindings :one
SELECT COUNT(*)::BIGINT
FROM risk_results rr
JOIN risk_policies rp ON rp.id = rr.risk_policy_id AND rp.deleted IS FALSE AND rp.enabled IS TRUE
WHERE rr.project_id = @project_id
  AND rr.found IS TRUE;

-- name: GetRiskOverviewCounts :one
SELECT
    COUNT(DISTINCT rr.chat_message_id)::BIGINT AS messages_scanned
  , (COUNT(*) FILTER (
      WHERE rr.found IS TRUE
    ))::BIGINT AS findings
  , (COUNT(DISTINCT cm.chat_id) FILTER (
      WHERE rr.found IS TRUE
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
  AND rr.found IS TRUE
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
    AND rr.found IS TRUE
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
  AND rr.found IS TRUE
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
    AND rr.found IS TRUE
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
    AND rr.found IS TRUE
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
    AND rr.found IS TRUE
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
SELECT id, content, tool_calls
FROM chat_messages
WHERE id = ANY(@ids::uuid[])
  AND project_id = @project_id;

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
  , @dead_letter_reason
);

-- name: DeleteRiskResultsForMessages :exec
DELETE FROM risk_results
WHERE risk_policy_id = @risk_policy_id
  AND project_id = @project_id
  AND chat_message_id = ANY(@message_ids::uuid[]);

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
SELECT
    sub.id, sub.project_id, sub.organization_id, sub.risk_policy_id,
    sub.risk_policy_version, sub.chat_message_id, sub.source, sub.found,
    sub.rule_id, sub.description, sub.match, sub.start_pos, sub.end_pos,
    sub.confidence, sub.tags, sub.dead_letter_reason, sub.created_at,
    sub.chat_id, sub.message_created_at, sub.chat_title, sub.chat_user_id
FROM (
  SELECT
      rr.id, rr.project_id, rr.organization_id, rr.risk_policy_id,
      rr.risk_policy_version, rr.chat_message_id, rr.source, rr.found,
      rr.rule_id, rr.description, rr.match, rr.start_pos, rr.end_pos,
      rr.confidence, rr.tags, rr.dead_letter_reason, rr.created_at,
      cm.chat_id, cm.created_at AS message_created_at,
      c.title AS chat_title, c.external_user_id AS chat_user_id,
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
  JOIN risk_policies rp ON rp.id = rr.risk_policy_id AND rp.deleted IS FALSE AND rp.enabled IS TRUE
  WHERE rr.project_id = @project_id
    AND rr.found IS TRUE
    AND (sqlc.narg(from_time)::timestamptz IS NULL OR cm.created_at >= sqlc.narg(from_time)::timestamptz)
    AND (sqlc.narg(to_time)::timestamptz IS NULL OR cm.created_at < sqlc.narg(to_time)::timestamptz)
    AND (@rule_id::text = '' OR rr.rule_id ILIKE '%' || @rule_id::text || '%')
    AND (@category::text = '' OR (
    CASE
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
SELECT rr.*, cm.chat_id, cm.created_at AS message_created_at, c.title AS chat_title, c.external_user_id AS chat_user_id
FROM risk_results rr
JOIN chat_messages cm ON cm.id = rr.chat_message_id
LEFT JOIN chats c ON c.id = cm.chat_id AND c.deleted IS FALSE
JOIN risk_policies rp ON rp.id = rr.risk_policy_id AND rp.deleted IS FALSE AND rp.enabled IS TRUE
WHERE rr.project_id = @project_id
  AND rr.risk_policy_id = @risk_policy_id
  AND rr.found IS TRUE
  AND (
    sqlc.narg(cursor_message_created_at)::timestamptz IS NULL
    OR (cm.created_at, rr.id) < (sqlc.narg(cursor_message_created_at)::timestamptz, sqlc.narg(cursor_id)::uuid)
  )
ORDER BY cm.created_at DESC, rr.id DESC
LIMIT @page_limit;

-- name: ListRiskResultsByChatFound :many
SELECT rr.*, cm.chat_id, cm.created_at AS message_created_at, c.title AS chat_title, c.external_user_id AS chat_user_id
FROM risk_results rr
JOIN chat_messages cm ON cm.id = rr.chat_message_id
LEFT JOIN chats c ON c.id = cm.chat_id AND c.deleted IS FALSE
JOIN risk_policies rp ON rp.id = rr.risk_policy_id AND rp.deleted IS FALSE AND rp.enabled IS TRUE
WHERE cm.chat_id = @chat_id
  AND rr.project_id = @project_id
  AND rr.found IS TRUE
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
  AND rr.found IS TRUE
  AND (sqlc.narg(cursor)::uuid IS NULL OR cm.chat_id <= sqlc.narg(cursor)::uuid)
GROUP BY cm.chat_id, c.title, c.external_user_id
ORDER BY cm.chat_id DESC
LIMIT @page_limit;

-- name: ListEnabledEnforcingPoliciesByProject :many
SELECT *
FROM risk_policies
WHERE project_id = @project_id
  AND enabled IS TRUE
  AND action = 'block'
  AND deleted IS FALSE;

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
