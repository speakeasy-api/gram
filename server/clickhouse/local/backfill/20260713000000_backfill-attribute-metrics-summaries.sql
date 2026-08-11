-- Production runbook: full re-derive of attribute_metrics_summaries with a
-- lossless, flip-based rollback (a keyed variant of the Altinity "delete via
-- tombstone column" pattern,
-- https://kb.altinity.com/altinity-kb-queries-and-syntax/delete-via-tombstone-column/).
--
-- Ships alongside the 20260713200603_attribute-metrics-tombstone-rollback
-- migration, which:
--   * adds `generation UInt8` to the SORTING KEY — an immutable discriminator
--     (0 = original/MV rows, 1 = this backfill). Rows of different
--     generations never merge, so the two copies of history coexist
--     untouched;
--   * adds `is_active UInt8 DEFAULT 1` (non-key, mutable) — the visibility
--     switch readers filter on (is_active = 1);
--   * moves the MV ingestion cutoff to 2026-07-14 00:00:00 UTC. The MV owns
--     event time >= the cutoff; this runbook owns everything before it, and
--     the cutoff is a whole-hour boundary so time_bucket partitions cleanly.
--
-- The flow: stage the re-derived data as generation 1 / hidden, verify it in
-- place, then CUT OVER by flipping is_active on both generations. Rollback is
-- the same flip in reverse — nothing is deleted until the optional final
-- cleanup, so it can be repeated any number of times.
--
-- INVARIANT for every mutation below: is_active is not part of the sorting
-- key, so its UPDATE predicates must only reference sort-key columns
-- (generation, time_bucket). Then rows with identical keys always carry the
-- same flag and a merge can never combine a hidden row with a visible one.
--
-- Do not start before BOTH:
--   * the migration is deployed, AND
--   * the wall clock is past 2026-07-14 00:00:00 UTC (waiting longer also
--     lets late-arriving pre-cutoff events land in telemetry_logs so step 1
--     picks them up; events arriving after step 1 runs are lost — a bounded
--     undercount, the safe failure direction).

-- ============================================================================
-- Step 0 — record the before-state for verification.
-- ============================================================================

SELECT generation, is_active, count() AS rows, min(time_bucket) AS oldest, max(time_bucket) AS newest
FROM attribute_metrics_summaries
GROUP BY generation, is_active;

-- ============================================================================
-- Step 1 — stage the backfill: re-derive every pre-cutoff bucket from
-- telemetry_logs (bounded by its 90-day TTL) with the provenance-first logic,
-- as generation 1, HIDDEN (is_active = 0). Readers filter is_active = 1, so
-- the costs page is untouched; generation 1 in the sorting key keeps these
-- rows from ever merging with the live generation-0 rows.
--
-- Mirrors attribute_metrics_summaries_mv (server/clickhouse/schema.sql)
-- exactly, with the cutoff condition inverted — keep the two in sync if the
-- MV changes before this runs.
-- ============================================================================

INSERT INTO attribute_metrics_summaries (gram_project_id, time_bucket, department_name, job_title, employee_type, division_name, cost_center_name, user_email, model, hook_source, roles, groups, total_chats, total_input_tokens, total_output_tokens, total_tokens, cache_read_input_tokens, cache_creation_input_tokens, total_cost, total_tool_calls, unique_tool_calls, account_type, provider, billing_mode, query_source, skill_name, agent_name, mcp_server_name, mcp_tool_name, generation, is_active)
WITH
    toUnixTimestamp64Nano(toDateTime64('2026-07-14 00:00:00', 9, 'UTC')) AS attribute_metrics_cutoff_unix_nano,
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
    ) AS tool_call_dedup_id
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
    account_type,
    provider,
    billing_mode,
    if(is_claude_api_request, toString(attributes.query_source), '') AS query_source,
    if(is_claude_api_request, toString(attributes.skill.name), '') AS skill_name,
    if(is_claude_api_request, toString(attributes.agent.name), '') AS agent_name,
    if(is_claude_api_request, toString(attributes.mcp_server.name), '') AS mcp_server_name,
    if(is_claude_api_request, toString(attributes.mcp_tool.name), '') AS mcp_tool_name,
    1 AS generation,
    0 AS is_active
FROM telemetry_logs
WHERE time_unix_nano < attribute_metrics_cutoff_unix_nano
  AND (is_claude_api_request OR is_claude_tool_result OR is_agent_usage_row OR is_agent_tool_call)
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
    account_type,
    provider,
    billing_mode,
    query_source,
    skill_name,
    agent_name,
    mcp_server_name,
    mcp_tool_name;

-- ============================================================================
-- Step 2 — verify the staged generation in place (it is invisible to the
-- dashboard, so take your time). Note min(time_bucket) of generation 1: raw
-- logs only reach back ~90 days, while generation 0 may hold older history
-- from the original out-of-band backfill — the cutover below deliberately
-- hides generation 0 only where generation 1 has replacement coverage, so
-- that older history stays visible.
-- ============================================================================

SELECT generation, count() AS rows, min(time_bucket) AS oldest, max(time_bucket) AS newest
FROM attribute_metrics_summaries
GROUP BY generation;

-- Per-day cost, staged vs live, for spot-checking (expect differences where
-- the provenance-first logic intentionally diverges from old ingestion).
SELECT toStartOfDay(time_bucket) AS day,
       round(sumIf(finalizeAggregation(total_cost), generation = 0), 2) AS live_cost,
       round(sumIf(finalizeAggregation(total_cost), generation = 1), 2) AS staged_cost
FROM attribute_metrics_summaries
WHERE time_bucket < toDateTime('2026-07-14 00:00:00', 'UTC')
GROUP BY day
ORDER BY day DESC;

-- ============================================================================
-- Step 3 — CUTOVER: flip visibility. Hide the original rows first, then
-- reveal the staged ones (a moment of undercount instead of double count).
-- Replace <OLDEST_STAGED_BUCKET> with min(time_bucket) of generation 1 from
-- step 2 so generation-0 history older than the raw TTL horizon stays
-- visible.
-- ============================================================================

ALTER TABLE attribute_metrics_summaries
    UPDATE is_active = 0
    WHERE generation = 0
      AND time_bucket >= toDateTime('<OLDEST_STAGED_BUCKET>', 'UTC')
      AND time_bucket < toDateTime('2026-07-14 00:00:00', 'UTC')
    SETTINGS mutations_sync = 2;

ALTER TABLE attribute_metrics_summaries
    UPDATE is_active = 1
    WHERE generation = 1
    SETTINGS mutations_sync = 2;

-- Verify, then spot-check the costs page.
SELECT generation, is_active, count() AS rows, min(time_bucket) AS oldest, max(time_bucket) AS newest
FROM attribute_metrics_summaries
GROUP BY generation, is_active;

-- ============================================================================
-- ROLLBACK — the reverse flips, lossless and repeatable: hide the backfill,
-- restore the original rows. (Order again favors undercount over double
-- count.) To retry a corrected backfill afterwards, hard-delete generation 1
-- (`ALTER TABLE attribute_metrics_summaries DELETE WHERE generation = 1
-- SETTINGS mutations_sync = 2`) and start over from step 1.
-- ============================================================================

-- ALTER TABLE attribute_metrics_summaries
--     UPDATE is_active = 0
--     WHERE generation = 1
--     SETTINGS mutations_sync = 2;
--
-- ALTER TABLE attribute_metrics_summaries
--     UPDATE is_active = 1
--     WHERE generation = 0
--       AND time_bucket < toDateTime('2026-07-14 00:00:00', 'UTC')
--     SETTINGS mutations_sync = 2;

-- ============================================================================
-- Step 4 — OPTIONAL cleanup, days/weeks later once fully confident: drop the
-- hidden generation-0 rows. This is the only destructive statement in the
-- runbook — after it, rollback is no longer possible.
-- ============================================================================

-- ALTER TABLE attribute_metrics_summaries
--     DELETE WHERE is_active = 0
--     SETTINGS mutations_sync = 2;
