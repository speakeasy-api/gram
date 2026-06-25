-- Read-only export read-model for the governed risk-clustering export workflow.
-- Every query here is SELECT-only and runs against the read replica. Unlike the
-- rest of the codebase these queries are org-scoped (with an optional project
-- filter) rather than strictly project-scoped, because an export deliberately
-- spans every project in an organization; the workflow gates access via RBAC
-- and audit logging instead.

-- name: SelectSampledChatIDs :many
-- Keyset page of sampled chat IDs matching the filters, oldest-id first.
-- Sampling is deterministic: a fixed (filters, seed, pct) triple reproduces the
-- same keep-set because the keep decision is a pure hash of the chat id. Pass
-- @sample_pct >= 100 to disable sampling. Callers fetch @lim+1 rows to detect
-- whether more pages remain (N+1 backpressure probe).
SELECT c.id
FROM chats c
WHERE c.organization_id = @organization_id
  AND c.deleted_at IS NULL
  AND (sqlc.narg('project_id')::uuid IS NULL OR c.project_id = sqlc.narg('project_id')::uuid)
  AND (sqlc.narg('created_from')::timestamptz IS NULL OR c.created_at >= sqlc.narg('created_from')::timestamptz)
  AND (sqlc.narg('created_to')::timestamptz IS NULL OR c.created_at < sqlc.narg('created_to')::timestamptz)
  AND (sqlc.narg('external_user_id')::text IS NULL OR c.external_user_id = sqlc.narg('external_user_id')::text)
  AND (
    @sample_pct::int >= 100
    OR abs(hashtextextended(c.id::text, @sample_seed::bigint)) % 100 < @sample_pct::int
  )
  AND (
    NOT @has_findings_only::bool
    OR EXISTS (
      SELECT 1
      FROM chat_messages cm
      JOIN risk_results rr ON rr.chat_message_id = cm.id
      LEFT JOIN risk_custom_detection_rules cr
        ON cr.project_id = rr.project_id
       AND cr.rule_id = rr.rule_id
       AND cr.deleted IS FALSE
      WHERE cm.chat_id = c.id
        AND rr.found IS TRUE
        AND rr.excluded_at IS NULL
        AND rr.false_positive_at IS NULL
        AND (sqlc.narg('risk_policy_id')::uuid IS NULL OR rr.risk_policy_id = sqlc.narg('risk_policy_id')::uuid)
        AND (cardinality(@rule_ids::text[]) = 0 OR rr.rule_id = ANY(@rule_ids::text[]))
        AND (cardinality(@sources::text[]) = 0 OR rr.source = ANY(@sources::text[]))
        AND (cardinality(@severities::text[]) = 0 OR cr.severity = ANY(@severities::text[]))
    )
  )
  AND (sqlc.narg('after_id')::uuid IS NULL OR c.id > sqlc.narg('after_id')::uuid)
ORDER BY c.id ASC
LIMIT @lim::int;

-- name: CountSampledChats :one
-- Population count for the same predicate as SelectSampledChatIDs, used for the
-- dry-run gate and the audit record. No keyset/limit.
SELECT count(*)::bigint AS total
FROM chats c
WHERE c.organization_id = @organization_id
  AND c.deleted_at IS NULL
  AND (sqlc.narg('project_id')::uuid IS NULL OR c.project_id = sqlc.narg('project_id')::uuid)
  AND (sqlc.narg('created_from')::timestamptz IS NULL OR c.created_at >= sqlc.narg('created_from')::timestamptz)
  AND (sqlc.narg('created_to')::timestamptz IS NULL OR c.created_at < sqlc.narg('created_to')::timestamptz)
  AND (sqlc.narg('external_user_id')::text IS NULL OR c.external_user_id = sqlc.narg('external_user_id')::text)
  AND (
    @sample_pct::int >= 100
    OR abs(hashtextextended(c.id::text, @sample_seed::bigint)) % 100 < @sample_pct::int
  )
  AND (
    NOT @has_findings_only::bool
    OR EXISTS (
      SELECT 1
      FROM chat_messages cm
      JOIN risk_results rr ON rr.chat_message_id = cm.id
      LEFT JOIN risk_custom_detection_rules cr
        ON cr.project_id = rr.project_id
       AND cr.rule_id = rr.rule_id
       AND cr.deleted IS FALSE
      WHERE cm.chat_id = c.id
        AND rr.found IS TRUE
        AND rr.excluded_at IS NULL
        AND rr.false_positive_at IS NULL
        AND (sqlc.narg('risk_policy_id')::uuid IS NULL OR rr.risk_policy_id = sqlc.narg('risk_policy_id')::uuid)
        AND (cardinality(@rule_ids::text[]) = 0 OR rr.rule_id = ANY(@rule_ids::text[]))
        AND (cardinality(@sources::text[]) = 0 OR rr.source = ANY(@sources::text[]))
        AND (cardinality(@severities::text[]) = 0 OR cr.severity = ANY(@severities::text[]))
    )
  );

-- name: ExportFindingCentric :many
-- Finding-centric export for a batch of chats. For each (chat_id, generation),
-- returns every message within +/- @context_size ordinal positions of an active
-- finding that matches the policy/rule/source filters, denormalized with the
-- finding plus its policy and custom-rule metadata. This reuses the windowing
-- idiom from chat.ListRiskWindowedMessages: rn is the 1-based ordinal within the
-- generation, total is the generation's message count, is_seed marks the rows
-- whose own finding anchored a window (context rows are is_seed = false).
-- Overlapping windows merge naturally via set membership. A context message with
-- no active finding emits one row with null finding columns; a message with
-- several findings emits one row per finding.
WITH ordered AS (
  SELECT
    cm.id,
    cm.chat_id,
    cm.seq,
    cm.generation,
    cm.role,
    cm.content,
    cm.content_raw,
    cm.content_asset_url,
    cm.model,
    cm.tool_calls,
    cm.tool_urn,
    cm.source,
    cm.external_user_id,
    cm.created_at,
    row_number() OVER (PARTITION BY cm.chat_id, cm.generation ORDER BY cm.seq) AS rn,
    count(*) OVER (PARTITION BY cm.chat_id, cm.generation) AS total
  FROM chat_messages cm
  WHERE cm.chat_id = ANY(@chat_ids::uuid[])
),
seed_rns AS (
  SELECT DISTINCT o.chat_id, o.generation, o.rn
  FROM ordered o
  WHERE EXISTS (
    SELECT 1
    FROM risk_results rr
    WHERE rr.chat_message_id = o.id
      AND rr.found IS TRUE
      AND rr.excluded_at IS NULL
      AND rr.false_positive_at IS NULL
      AND (sqlc.narg('risk_policy_id')::uuid IS NULL OR rr.risk_policy_id = sqlc.narg('risk_policy_id')::uuid)
      AND (cardinality(@rule_ids::text[]) = 0 OR rr.rule_id = ANY(@rule_ids::text[]))
      AND (cardinality(@sources::text[]) = 0 OR rr.source = ANY(@sources::text[]))
  )
)
SELECT
  o.chat_id,
  o.id AS message_id,
  o.seq,
  o.generation,
  o.rn,
  o.total,
  o.role,
  o.content,
  o.content_raw,
  o.content_asset_url,
  o.model,
  o.tool_calls,
  o.tool_urn,
  o.source,
  o.external_user_id,
  o.created_at,
  EXISTS (SELECT 1 FROM seed_rns s WHERE s.chat_id = o.chat_id AND s.generation = o.generation AND s.rn = o.rn) AS is_seed,
  rr.id AS finding_id,
  rr.risk_policy_id,
  rr.risk_policy_version,
  rr.rule_id AS finding_rule_id,
  rr.source AS finding_source,
  rr.description AS finding_description,
  rr.match AS finding_match,
  rr.start_pos,
  rr.end_pos,
  rr.confidence,
  rr.tags,
  rr.spans,
  rp.name AS policy_name,
  rp.policy_type,
  rp.action AS policy_action,
  cr.title AS rule_title,
  cr.severity AS rule_severity
FROM ordered o
LEFT JOIN risk_results rr
  ON rr.chat_message_id = o.id
 AND rr.found IS TRUE
 AND rr.excluded_at IS NULL
 AND rr.false_positive_at IS NULL
LEFT JOIN risk_policies rp ON rp.id = rr.risk_policy_id
LEFT JOIN risk_custom_detection_rules cr
  ON cr.project_id = rr.project_id
 AND cr.rule_id = rr.rule_id
 AND cr.deleted IS FALSE
WHERE EXISTS (
    SELECT 1
    FROM seed_rns s
    WHERE s.chat_id = o.chat_id
      AND s.generation = o.generation
      AND o.rn BETWEEN s.rn - @context_size::bigint AND s.rn + @context_size::bigint
  )
  AND (cardinality(@roles::text[]) = 0 OR o.role = ANY(@roles::text[]))
  AND (cardinality(@models::text[]) = 0 OR o.model = ANY(@models::text[]))
ORDER BY o.chat_id, o.generation, o.seq ASC, rr.id ASC;

-- name: ExportFullTranscript :many
-- Full-transcript export for a batch of chats: every message LEFT JOINed to its
-- active findings (plus policy and custom-rule metadata). Carries chat_id, seq
-- and generation so the offline consumer can reassemble whole transcripts.
SELECT
  cm.chat_id,
  cm.id AS message_id,
  cm.seq,
  cm.generation,
  cm.role,
  cm.content,
  cm.content_raw,
  cm.content_asset_url,
  cm.model,
  cm.tool_calls,
  cm.tool_urn,
  cm.source,
  cm.external_user_id,
  cm.created_at,
  rr.id AS finding_id,
  rr.risk_policy_id,
  rr.risk_policy_version,
  rr.rule_id AS finding_rule_id,
  rr.source AS finding_source,
  rr.description AS finding_description,
  rr.match AS finding_match,
  rr.start_pos,
  rr.end_pos,
  rr.confidence,
  rr.tags,
  rr.spans,
  rp.name AS policy_name,
  rp.policy_type,
  rp.action AS policy_action,
  cr.title AS rule_title,
  cr.severity AS rule_severity
FROM chat_messages cm
LEFT JOIN risk_results rr
  ON rr.chat_message_id = cm.id
 AND rr.found IS TRUE
 AND rr.excluded_at IS NULL
 AND rr.false_positive_at IS NULL
LEFT JOIN risk_policies rp ON rp.id = rr.risk_policy_id
LEFT JOIN risk_custom_detection_rules cr
  ON cr.project_id = rr.project_id
 AND cr.rule_id = rr.rule_id
 AND cr.deleted IS FALSE
WHERE cm.chat_id = ANY(@chat_ids::uuid[])
  AND (cardinality(@roles::text[]) = 0 OR cm.role = ANY(@roles::text[]))
  AND (cardinality(@models::text[]) = 0 OR cm.model = ANY(@models::text[]))
  AND (cardinality(@msg_sources::text[]) = 0 OR cm.source = ANY(@msg_sources::text[]))
ORDER BY cm.chat_id, cm.generation, cm.seq ASC, rr.id ASC;
