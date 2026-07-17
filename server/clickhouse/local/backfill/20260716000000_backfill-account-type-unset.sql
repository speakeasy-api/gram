-- Production runbook: reclassify a customer org's historical "(unset)"
-- account-type spend as team on attribute_metrics_summaries (POC-305), with
-- the same lossless, flip-based rollback used by the 2026-07 full re-derive
-- (20260713000000_backfill-attribute-metrics-summaries.sql — read that file's
-- header first; the generation/is_active machinery is documented there and in
-- the column comments in server/clickhouse/schema.sql).
--
-- CONFIDENTIALITY: this file deliberately contains NO customer-identifying
-- values (no customer name, project id, or spend figures). <PROJECT_ID> is a
-- placeholder; the concrete project id, the verified prod numbers, and the
-- expected before/after split live in the Linear ticket (POC-305). Fill in
-- the placeholder at execution time and keep it out of anything committed.
--
-- WHY: when Claude Code runs against company credentials (API key,
-- gateway/proxy, Bedrock, Vertex) it never emits user.account_uuid, and until
-- speakeasy-api/gram#4259 (merged 2026-07-16) attributeSession early-returned
-- on the empty UUID before classifying — so those sessions were stamped with
-- an empty account_type and the Costs page account-type breakdown parked
-- nearly all of the affected org's spend under "(unset)". #4259 fixes ingest
-- going forward (empty UUID => team); this runbook applies the same rule to
-- the historical rows.
--
-- WHAT MOVES: only aggregate rows keyed account_type = '' whose source rows
-- are Claude OTEL rows from sessions that never carried a user.account_uuid.
-- Deliberately left as "(unset)":
--   * Codex/Cursor rows — they never run account attribution (matching #4259's
--     scope; a separate feature).
--   * Claude rows from sessions that DID carry a UUID somewhere (a mid-session
--     OAuth login): their pre-enrichment rows keep the empty stamp, mirroring
--     the ingest behavior. A tiny slice — at the affected org the overwhelming
--     majority of sessions carry no UUID (verified counts in POC-305).
--   * Buckets older than the telemetry_logs 90-day TTL horizon — there is no
--     raw data left to re-derive them from, so they stay visible untouched
--     rather than silently disappearing.
-- Raw telemetry_logs rows are NOT touched: account_type there is a
-- MATERIALIZED column (not mutable), so raw-path group-bys keep showing the
-- historical "(unset)" until the 90-day TTL ages it out. Filter semantics are
-- already correct everywhere: withAccountTypeFilter treats empty as team
-- ("ifNull(account_type, '') != 'personal'").
--
-- MECHANICS: account_type is part of the sorting key, so the "(unset)" bucket
-- rows cannot be UPDATEd in place. Instead we stage replacement rows as
-- generation 3 (hidden), re-derived from telemetry_logs with the corrected
-- account_type, then flip visibility: hide the project's generation-0/1
-- account_type = '' rows in the covered window, reveal generation 3.
-- Generation 3 is DEDICATED to this backfill — live MV rows are generation 2
-- (or 0 before the gen2 MV migration), so staged rows never share a
-- generation with anything else: the guards, the reveal, and the rollback all
-- select exactly the staged rows and nothing more. Existing
-- team/personal rows are never touched — the staged team rows simply coexist
-- with them and the query layer's -Merge aggregation combines the states.
--
-- INVARIANT (same as the parent runbook): is_active UPDATE predicates must
-- only reference sort-key columns. Every mutation below predicates on
-- gram_project_id, generation, account_type, and time_bucket — all in the
-- sorting key.
--
-- PARAMETERS:
--   <PROJECT_ID>           the affected org's gram project id (step 0a; the
--                          verified value is recorded in POC-305). If the org
--                          has several projects with unset spend, run the
--                          whole runbook once per project id — prod
--                          verification found only one that matters (POC-305).
--   window upper bound     2026-07-16 05:00:00 UTC, baked into the statements
--                          below — the whole hour after the #4259 deploy
--                          started stamping team at 04:55:17 UTC (step 0b
--                          re-confirms). Claude rows after the bound are
--                          stamped at ingest and stay untouched.
--   <OLDEST_STAGED_BUCKET> the one run-time parameter: min(time_bucket) of
--                          generation 3 after step 1 (step 2 tells you) —
--                          bounds the hide flip so any history the raw logs
--                          cannot re-derive stays visible. Prod verification
--                          confirmed the org's history begins well inside the
--                          raw 90-day TTL, so the whole window is
--                          re-derivable.
--
-- Prod state was verified on 2026-07-16 (read replica + ClickHouse read-only;
-- concrete numbers in POC-305). Step 0 re-confirms the preconditions at run
-- time: #4259 stamping team (0b), the 20260713 re-derive cutover end state
-- (0c), and — REQUIRED before staging — the 20260717 gen2-attribute-metrics-mv
-- migration deployed (ships with this runbook; it recreates the MV stamping
-- generation 2 instead of 0). With that live, every row ingested after step 1
-- lands as generation 2 / active and is structurally immune to the cutover's
-- generation-0/1 hide flip — no late-arriving row can be hidden without a
-- staged replacement. Anything else: stop.

-- ============================================================================
-- Step 0a — find the affected org's project id (Postgres, prod read replica;
-- substitute the customer's name from POC-305 into the ILIKE):
--
--   SELECT p.id, p.name, p.slug, o.name AS org_name
--   FROM projects p
--   JOIN organization_metadata o ON o.id = p.organization_id
--   WHERE o.name ILIKE '%<customer>%' AND NOT p.deleted;
--
-- Then confirm which project(s) actually carry the unset spend:
-- ============================================================================

SELECT gram_project_id,
       count() AS unset_rows,
       round(sumIf(finalizeAggregation(total_cost), is_active = 1), 2) AS live_unset_cost
FROM attribute_metrics_summaries
WHERE account_type = ''
  AND gram_project_id IN (toUUID('<PROJECT_ID>') /* , more ids from the Postgres lookup */)
GROUP BY gram_project_id;

-- ============================================================================
-- Step 0b — confirm the #4259 fix is live: the first UUID-less Claude row
-- stamped team. Expect 2026-07-16 04:55:17 UTC — the baked window upper bound
-- (2026-07-16 05:00:00) is the whole hour right after it.
-- ============================================================================

SELECT min(fromUnixTimestamp64Nano(time_unix_nano)) AS first_uuidless_team_row
FROM telemetry_logs
WHERE gram_project_id = toUUID('<PROJECT_ID>')
  AND gram_urn = 'claude-code:otel:logs'
  AND account_type = 'team'
  AND toString(attributes.user.account_uuid) = ''
  AND time_unix_nano >= toUnixTimestamp64Nano(toDateTime64('2026-07-16 00:00:00', 9, 'UTC'));

-- ============================================================================
-- Step 0c — record the before-state. Expected shape for the project:
--   generation 0, is_active 0 — pre-2026-07-14 rows hidden by the 20260713
--                               cutover (absent if its final cleanup ran);
--   generation 0, is_active 1 — MV rows from 2026-07-14 until the
--                               gen2-attribute-metrics-mv migration deployed;
--   generation 1, is_active 1 — the 20260713 re-derive;
--   generation 2, is_active 1 — live MV rows since that migration deployed
--                               (their presence CONFIRMS the migration
--                               precondition);
--   no generation 3 rows (a prior staging attempt) yet.
-- Anything else: stop and reconcile before proceeding.
-- ============================================================================

SELECT generation, is_active, count() AS rows, min(time_bucket) AS oldest, max(time_bucket) AS newest
FROM attribute_metrics_summaries
WHERE gram_project_id = toUUID('<PROJECT_ID>')
GROUP BY generation, is_active
ORDER BY generation, is_active;

-- Size the slice that will move: distinct Claude sessions with vs without a
-- user.account_uuid (expected magnitudes recorded in POC-305).
SELECT
    uniqExactIf(chat_id, chat_id != '' AND max_uuid != '') AS sessions_with_uuid,
    uniqExactIf(chat_id, chat_id != '' AND max_uuid = '') AS sessions_without_uuid
FROM (
    SELECT chat_id, max(toString(attributes.user.account_uuid)) AS max_uuid
    FROM telemetry_logs
    WHERE gram_project_id = toUUID('<PROJECT_ID>')
      AND gram_urn = 'claude-code:otel:logs'
    GROUP BY chat_id
);

-- ============================================================================
-- Step 1 — stage the replacement rows: re-derive ONLY the source rows that fed
-- the "(unset)" bucket (account_type = '' — the corrected value deliberately
-- carries a different alias, corrected_account_type, so it can never shadow
-- the source column in WHERE), with the corrected classification, as
-- generation 2, HIDDEN (is_active = 0).
--
-- Everything except the WHERE restriction and corrected_account_type mirrors
-- attribute_metrics_summaries_mv (server/clickhouse/schema.sql) — keep them in
-- sync if the MV changes before this runs.
--
-- STAGING IS NOT IDEMPOTENT: a second run of the INSERT merges duplicate
-- aggregate states into the same generation-3 keys and doubles the staged
-- spend. Two ENFORCING guards prevent that: the throwIf below errors when
-- generation-3 rows already exist (aborting a piped multi-statement run
-- before the INSERT), and the INSERT's own WHERE repeats the check as a
-- scalar-subquery predicate so a re-run inserts 0 rows even when the INSERT
-- is executed on its own. Generation 3 belongs to this backfill alone (live
-- MV rows are generation 2), so ANY generation-3 row for the project — hidden
-- or already cut over — means a prior staging attempt, and the guards protect
-- before AND after cutover. If one exists, hard-delete it first
-- (`ALTER TABLE attribute_metrics_summaries DELETE WHERE gram_project_id =
-- toUUID('<PROJECT_ID>') AND generation = 3 SETTINGS mutations_sync = 2`) and only
-- then re-run the INSERT.
-- ============================================================================

SELECT throwIf(
    count() > 0,
    'generation 3 already staged for this project - hard-delete it before re-staging (see comment above)'
) AS stage_once_guard
FROM attribute_metrics_summaries
WHERE gram_project_id = toUUID('<PROJECT_ID>')
  AND generation = 3;

INSERT INTO attribute_metrics_summaries (gram_project_id, time_bucket, department_name, job_title, employee_type, division_name, cost_center_name, user_email, model, hook_source, roles, groups, total_chats, total_input_tokens, total_output_tokens, total_tokens, cache_read_input_tokens, cache_creation_input_tokens, total_cost, total_tool_calls, unique_tool_calls, account_type, provider, billing_mode, query_source, skill_name, agent_name, mcp_server_name, mcp_tool_name, generation, is_active)
WITH
    toUnixTimestamp64Nano(toDateTime64('2026-07-16 05:00:00', 9, 'UTC')) AS backfill_boundary_unix_nano,
    (gram_urn = 'claude-code:otel:logs') AS is_claude_otel_row,
    (
        is_claude_otel_row
        AND chat_id != ''
        AND toString(attributes.prompt.id) != ''
        AND (toString(attributes.event.name) = 'api_request' OR body = 'claude_code.api_request')
    ) AS is_claude_api_request,
    (
        is_claude_otel_row
        AND (toString(attributes.event.name) = 'tool_result' OR body = 'claude_code.tool_result')
    ) AS is_claude_tool_result,
    (startsWith(gram_urn, 'codex:usage') OR startsWith(gram_urn, 'cursor:usage')) AS is_agent_usage_row,
    (
        toString(attributes.gram.hook.source) IN ('codex', 'cursor')
        AND toString(attributes.gram.tool.name) != ''
        AND toString(attributes.gram.tool.name) NOT IN ('claude-code', 'codex', 'cursor')
        AND toString(attributes.gram.hook.event) IN ('PostToolUse', 'PostToolUseFailure')
    ) AS is_agent_tool_call,
    (is_claude_tool_result OR is_agent_tool_call) AS is_counted_tool_call,
    multiIf(
        toString(attributes.tool_use_id) != '', toString(attributes.tool_use_id),
        toString(attributes.gen_ai.tool.call.id) != '', toString(attributes.gen_ai.tool.call.id),
        toString(id)
    ) AS tool_call_dedup_id,
    -- The #4259 rule applied retroactively: a Claude OTEL row from a session
    -- that never carried user.account_uuid is company-credential => team.
    -- Session granularity (not row): the UUID rides only on some event types,
    -- so a row-level check would misclassify UUID-bearing sessions' quiet
    -- rows. Rows with no chat_id cannot be session-matched and fall back to
    -- the row-level attribute.
    (
        is_claude_otel_row
        AND if(
            chat_id != '',
            chat_id NOT IN (
                SELECT DISTINCT chat_id
                FROM telemetry_logs
                WHERE gram_project_id = toUUID('<PROJECT_ID>')
                  AND gram_urn = 'claude-code:otel:logs'
                  AND chat_id != ''
                  AND toString(attributes.user.account_uuid) != ''
            ),
            toString(attributes.user.account_uuid) = ''
        )
    ) AS is_company_credential_claude_row,
    -- Source rows are restricted to account_type = '' below, so the corrected
    -- value is either the team reclassification or the preserved empty stamp
    -- (Codex/Cursor rows and UUID-bearing sessions' pre-enrichment rows).
    if(is_company_credential_claude_row, 'team', '') AS corrected_account_type
SELECT
    gram_project_id,
    toStartOfHour(fromUnixTimestamp64Nano(time_unix_nano)) AS time_bucket,
    toString(attributes.user.attributes.department_name) AS department_name,
    toString(attributes.user.attributes.job_title) AS job_title,
    toString(attributes.user.attributes.employee_type) AS employee_type,
    toString(attributes.user.attributes.division_name) AS division_name,
    toString(attributes.user.attributes.cost_center_name) AS cost_center_name,
    user_email AS user_email,
    multiIf(
        is_claude_api_request AND toString(attributes.model) != '', toString(attributes.model),
        is_claude_api_request AND toString(attributes.gen_ai.request.model) != '', toString(attributes.gen_ai.request.model),
        toString(attributes.gen_ai.response.model)
    ) AS model,
    hook_source,
    arraySort(JSONExtract(ifNull(toJSONString(attributes.user.roles), '[]'), 'Array(String)')) AS roles,
    arraySort(JSONExtract(ifNull(toJSONString(attributes.user.groups), '[]'), 'Array(String)')) AS groups,
    uniqExactIfState(toString(attributes.gen_ai.conversation.id), toString(attributes.gen_ai.conversation.id) != '' AND (is_claude_api_request OR is_agent_usage_row)) AS total_chats,
    sumIfState(if(is_claude_api_request, toInt64OrZero(toString(attributes.input_tokens)), toInt64OrZero(toString(attributes.gen_ai.usage.input_tokens))), is_claude_api_request OR is_agent_usage_row) AS total_input_tokens,
    sumIfState(if(is_claude_api_request, toInt64OrZero(toString(attributes.output_tokens)), toInt64OrZero(toString(attributes.gen_ai.usage.output_tokens))), is_claude_api_request OR is_agent_usage_row) AS total_output_tokens,
    sumIfState(if(is_claude_api_request, toInt64OrZero(toString(attributes.input_tokens)) + toInt64OrZero(toString(attributes.output_tokens)) + toInt64OrZero(toString(attributes.cache_creation_tokens)), toInt64OrZero(toString(attributes.gen_ai.usage.input_tokens)) + toInt64OrZero(toString(attributes.gen_ai.usage.output_tokens)) + toInt64OrZero(toString(attributes.gen_ai.usage.cache_creation.input_tokens))), is_claude_api_request OR is_agent_usage_row) AS total_tokens,
    sumIfState(if(is_claude_api_request, toInt64OrZero(toString(attributes.cache_read_tokens)), toInt64OrZero(toString(attributes.gen_ai.usage.cache_read.input_tokens))), is_claude_api_request OR is_agent_usage_row) AS cache_read_input_tokens,
    sumIfState(if(is_claude_api_request, toInt64OrZero(toString(attributes.cache_creation_tokens)), toInt64OrZero(toString(attributes.gen_ai.usage.cache_creation.input_tokens))), is_claude_api_request OR is_agent_usage_row) AS cache_creation_input_tokens,
    sumIfState(if(is_claude_api_request, multiIf(toString(attributes.cost_usd) != '', toFloat64OrZero(toString(attributes.cost_usd)), toString(attributes.cost_usd_micros) != '', toFloat64OrZero(toString(attributes.cost_usd_micros)) / 1000000, 0), toFloat64OrZero(toString(attributes.gen_ai.usage.cost))), is_claude_api_request OR is_agent_usage_row) AS total_cost,
    countIfState(is_counted_tool_call) AS total_tool_calls,
    uniqExactIfState(tool_call_dedup_id, is_counted_tool_call) AS unique_tool_calls,
    corrected_account_type,
    provider,
    billing_mode,
    if(is_claude_api_request, toString(attributes.query_source), '') AS query_source,
    if(is_claude_api_request, toString(attributes.skill.name), '') AS skill_name,
    if(is_claude_api_request, toString(attributes.agent.name), '') AS agent_name,
    if(is_claude_api_request, toString(attributes.mcp_server.name), '') AS mcp_server_name,
    if(is_claude_api_request, toString(attributes.mcp_tool.name), '') AS mcp_tool_name,
    3 AS generation,
    0 AS is_active
FROM telemetry_logs
WHERE gram_project_id = toUUID('<PROJECT_ID>')
  AND time_unix_nano < backfill_boundary_unix_nano
  -- Only source rows that fed the "(unset)" bucket (indexed materialized column).
  AND account_type = ''
  AND (is_claude_api_request OR is_claude_tool_result OR is_agent_usage_row OR is_agent_tool_call)
  -- Stage-once guard, enforced in the INSERT itself: inserts 0 rows if a
  -- prior generation-3 staging already exists (generation 3 is dedicated to
  -- this backfill). See the comment above the throwIf.
  AND (
      SELECT count()
      FROM attribute_metrics_summaries
      WHERE gram_project_id = toUUID('<PROJECT_ID>')
        AND generation = 3
  ) = 0
GROUP BY
    gram_project_id,
    time_bucket,
    department_name,
    job_title,
    employee_type,
    division_name,
    cost_center_name,
    user_email,
    model,
    hook_source,
    roles,
    groups,
    corrected_account_type,
    provider,
    billing_mode,
    query_source,
    skill_name,
    agent_name,
    mcp_server_name,
    mcp_tool_name;

-- ============================================================================
-- Step 2 — verify the staged generation in place (hidden, so take your time).
-- Note min(time_bucket) of generation 3: that is <OLDEST_STAGED_BUCKET> for
-- the flips below. Buckets older than it (beyond the raw 90-day horizon) are
-- deliberately left visible as "(unset)".
-- ============================================================================

SELECT generation, count() AS rows, min(time_bucket) AS oldest, max(time_bucket) AS newest
FROM attribute_metrics_summaries
WHERE gram_project_id = toUUID('<PROJECT_ID>')
GROUP BY generation;

-- Conservation check, per day: the staged rows must carry (approximately) the
-- same cost as the live "(unset)" rows they replace — staged_cost may run
-- slightly HIGHER on pre-2026-07-14 days (late-arriving rows the 20260713
-- re-derive missed), never materially lower. staged_team_cost is the share
-- moving to team; the remainder stays "(unset)" by design. Compare against the
-- split previewed during prod verification (recorded in POC-305).
SELECT toStartOfDay(time_bucket) AS day,
       round(sumIf(finalizeAggregation(total_cost), generation IN (0, 1) AND account_type = '' AND is_active = 1), 2) AS live_unset_cost,
       round(sumIf(finalizeAggregation(total_cost), generation = 3), 2) AS staged_cost,
       round(sumIf(finalizeAggregation(total_cost), generation = 3 AND account_type = 'team'), 2) AS staged_team_cost
FROM attribute_metrics_summaries
WHERE gram_project_id = toUUID('<PROJECT_ID>')
  AND time_bucket < toDateTime('2026-07-16 05:00:00', 'UTC')
GROUP BY day
ORDER BY day DESC;

-- ============================================================================
-- Step 3 — CUTOVER: flip visibility, scoped to the project's "(unset)" bucket
-- in the covered window. Hide first, then reveal (a moment of undercount
-- instead of double count). generation IN (0, 1) covers both the post-cutoff
-- MV rows and the 20260713 re-derive; re-hiding already-hidden generation-0
-- pre-cutoff rows is a no-op.
--
-- LATE ARRIVALS: structurally closed by the gen2-attribute-metrics-mv
-- migration (a precondition — step 0c confirms). Every row ingested after
-- the migration lands as generation 2 / active, and the hide flip below only
-- targets generations 0/1 — so no row ingested after staging can be hidden
-- without a staged replacement. Still re-run the step-2 conservation query
-- immediately before the flips as a sanity gate: live_unset_cost materially
-- above staged_cost would mean staging ran BEFORE the migration deployed
-- (precondition violation) — roll back to the guards' comment, hard-delete
-- the staged rows, and restage.
-- ============================================================================

ALTER TABLE attribute_metrics_summaries
    UPDATE is_active = 0
    WHERE gram_project_id = toUUID('<PROJECT_ID>')
      AND generation IN (0, 1)
      AND account_type = ''
      AND time_bucket >= toDateTime('<OLDEST_STAGED_BUCKET>', 'UTC')
      AND time_bucket < toDateTime('2026-07-16 05:00:00', 'UTC')
    SETTINGS mutations_sync = 2;

-- Generation 3 holds only this backfill's staged rows, so the reveal needs no
-- further bounds and is the exact inverse of the rollback's re-hide below.
ALTER TABLE attribute_metrics_summaries
    UPDATE is_active = 1
    WHERE gram_project_id = toUUID('<PROJECT_ID>')
      AND generation = 3
    SETTINGS mutations_sync = 2;

-- Verify, then spot-check the affected org's Costs page account-type
-- breakdown: the "(unset)" share should collapse to the residual slice.
SELECT generation, is_active, count() AS rows, min(time_bucket) AS oldest, max(time_bucket) AS newest
FROM attribute_metrics_summaries
WHERE gram_project_id = toUUID('<PROJECT_ID>')
GROUP BY generation, is_active
ORDER BY generation, is_active;

SELECT account_type,
       round(sumIf(finalizeAggregation(total_cost), is_active = 1), 2) AS live_cost
FROM attribute_metrics_summaries
WHERE gram_project_id = toUUID('<PROJECT_ID>')
GROUP BY account_type;

-- ============================================================================
-- ROLLBACK — exact reverse flips, lossless and repeatable. This restores the
-- step-0c end state: generation 1 visible before 2026-07-14 (the 20260713
-- runbook's cutoff), generation 0 visible from it (generation-0 rows before it
-- were already hidden by that runbook's cutover and must stay hidden). To
-- retry a corrected backfill afterwards, hard-delete generation 3 for the
-- project (`ALTER TABLE attribute_metrics_summaries DELETE WHERE
-- gram_project_id = toUUID('<PROJECT_ID>') AND generation = 3 SETTINGS
-- mutations_sync = 2`) and start over from step 1.
-- ============================================================================

-- Generation 3 holds only this backfill's staged rows, so the re-hide is
-- EXACTLY lossless: no live MV row (generation 2, or 0/1 history) is touched.
--
-- ALTER TABLE attribute_metrics_summaries
--     UPDATE is_active = 0
--     WHERE gram_project_id = toUUID('<PROJECT_ID>')
--       AND generation = 3
--     SETTINGS mutations_sync = 2;
--
-- ALTER TABLE attribute_metrics_summaries
--     UPDATE is_active = 1
--     WHERE gram_project_id = toUUID('<PROJECT_ID>')
--       AND generation = 1
--       AND account_type = ''
--       AND time_bucket >= toDateTime('<OLDEST_STAGED_BUCKET>', 'UTC')
--       AND time_bucket < toDateTime('2026-07-14 00:00:00', 'UTC')
--     SETTINGS mutations_sync = 2;
--
-- ALTER TABLE attribute_metrics_summaries
--     UPDATE is_active = 1
--     WHERE gram_project_id = toUUID('<PROJECT_ID>')
--       AND generation = 0
--       AND account_type = ''
--       AND time_bucket >= toDateTime('2026-07-14 00:00:00', 'UTC')
--       AND time_bucket < toDateTime('2026-07-16 05:00:00', 'UTC')
--     SETTINGS mutations_sync = 2;

-- ============================================================================
-- Step 4 — OPTIONAL cleanup, days/weeks later once fully confident: drop the
-- project's hidden replaced rows. Destructive — after it, rollback is no
-- longer possible. Scoped tighter than a bare is_active = 0 sweep so it cannot
-- collide with other generations' hidden rows.
-- ============================================================================

-- ALTER TABLE attribute_metrics_summaries
--     DELETE WHERE gram_project_id = toUUID('<PROJECT_ID>')
--       AND generation IN (0, 1)
--       AND account_type = ''
--       AND is_active = 0
--       AND time_bucket >= toDateTime('<OLDEST_STAGED_BUCKET>', 'UTC')
--       AND time_bucket < toDateTime('2026-07-16 05:00:00', 'UTC')
--     SETTINGS mutations_sync = 2;
