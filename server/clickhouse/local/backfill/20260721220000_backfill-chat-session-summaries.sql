-- Production runbook: one-time history backfill for chat_session_summaries
-- (INC-417).
--
-- chat_session_summaries_mv only ingests rows with event time >= its cutoff
-- (2026-07-21 22:00:00 UTC). This runbook aggregates everything BEFORE the
-- cutoff out of raw telemetry_logs (bounded by its 90-day TTL), completing
-- the table's history. The INSERT mirrors the MV exactly — see
-- chat_session_summaries_mv in server/clickhouse/schema.sql and the session*
-- constants in server/internal/telemetry/repo/sessions.go; keep them in sync
-- if the MV changes before this runs.
--
-- Unlike the attribute-metrics re-derive runbook there is no tombstone /
-- generation machinery: the table is new and the pre-cutoff key space is
-- empty, so the cutoff itself is the writer boundary. The MV owns event
-- times >= cutoff, this runbook owns everything before it — disjoint sets,
-- no double count, no gap.
--
-- Execution order: preflight (step -1) -> expectations (step 0) -> reset +
-- insert (step 1, always run as one unit) -> verification (step 2). Do not
-- start before ALL of:
--   * the 20260721203159_chat-session-summaries migration is deployed;
--   * the wall clock is at least 1 HOUR past the effective boundary
--     established in step -1. The margin exists because Claude api_request
--     rows can wait up to ~30 minutes in telemetry_logs_staging before being
--     promoted into telemetry_logs (promoteStagedTelemetryTimeout in
--     server/internal/background/activities/promote_staged_telemetry.go);
--     rows promoted after step 1 with pre-boundary event times would be
--     missed by both writers. The step -1 staging-drain gate checks this
--     explicitly rather than by clock alone.
--
-- Step 1 is re-runnable as a UNIT (its reset DELETE wipes the range before
-- inserting), but never re-run the INSERT alone: aggregate states merge
-- additively, so an INSERT on top of existing rows doubles every sum.
--
-- The backfill floor (2026-04-24 00:00:00 UTC = cutoff - 89 days) exists
-- because chat_session_summaries carries a 90-day TTL that silently DROPS
-- expired buckets at insert time: without a floor, source rows older than
-- the TTL horizon (telemetry_logs prunes its own TTL lazily, so such rows
-- can still be present) would be counted by the step-0 expectations but
-- vanish from the insert, making the verification fail spuriously. The
-- floor keeps every counted bucket inside the TTL horizon PROVIDED step 1
-- runs before 2026-07-23 00:00 UTC — if running later, raise the floor so
-- it stays >= (execution time - 89 days), using the same value in every
-- statement of this file.

-- ============================================================================
-- Step -1 — preflight: establish the effective boundary and gate on it.
-- ============================================================================

-- -1a. When was the MV actually created? The MV only captures rows INSERTED
-- after it exists. If this timestamp is BEFORE the cutoff (the deploy beat
-- the clock), the effective boundary is the cutoff and the file runs as
-- written. If it is AFTER the cutoff, rows with event times in
-- [cutoff, creation) that were inserted before creation were captured by
-- neither writer — take the next whole hour >= this timestamp as the
-- effective boundary and replace '2026-07-21 22:00:00' in EVERY statement of
-- this file with it (step 1's reset DELETE then wipes the partial buckets
-- the MV wrote below the new boundary, so nothing double counts).
SELECT metadata_modification_time AS mv_created_at
FROM system.tables
WHERE database = currentDatabase() AND name = 'chat_session_summaries_mv';

-- -1b. Staging-drain gate: must return 0 before proceeding. Rows still
-- sitting in telemetry_logs_staging with pre-boundary event times will be
-- promoted into telemetry_logs later — after step 1 has already read it —
-- and, being evented before the boundary, will be skipped by the MV too:
-- permanently missing. If this returns > 0, wait for the promotion sweep to
-- drain and re-check.
SELECT count() AS undrained_pre_boundary_rows
FROM telemetry_logs_staging
WHERE time_unix_nano < toUnixTimestamp64Nano(toDateTime64('2026-07-21 22:00:00', 9, 'UTC'));

-- ============================================================================
-- Step 0 — record the before-state and the expected outcome.
-- ============================================================================

-- 0a. Before-state of the target: expect ONLY buckets >= the cutoff (live MV
-- rows) and zero rows below it. Record pre/post counts to diff after step 1.
SELECT
    countIf(time_bucket < toDateTime('2026-07-21 22:00:00', 'UTC')) AS pre_cutoff_rows,
    countIf(time_bucket >= toDateTime('2026-07-21 22:00:00', 'UTC')) AS post_cutoff_rows,
    min(time_bucket) AS oldest,
    max(time_bucket) AS newest
FROM chat_session_summaries;

-- 0b. Expected insert size from the source: the INSERT emits exactly one row
-- per distinct (project, hour bucket, chat) among admitted pre-cutoff rows.
-- pre_cutoff_rows after step 1 must equal expected_rows; expected_chats and
-- oldest/newest_bucket are recorded for the same comparison in step 2.
WITH
    toUnixTimestamp64Nano(toDateTime64('2026-07-21 22:00:00', 9, 'UTC')) AS chat_session_cutoff_unix_nano,
    toUnixTimestamp64Nano(toDateTime64('2026-04-24 00:00:00', 9, 'UTC')) AS chat_session_backfill_floor_unix_nano,
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
    (startsWith(gram_urn, 'codex:usage') OR startsWith(gram_urn, 'cursor:usage') OR startsWith(gram_urn, 'claude_chat:usage') OR startsWith(gram_urn, 'claude_chat:cost')) AS is_agent_usage_row,
    (
        hook_source IN ('codex', 'cursor')
        AND toString(attributes.gram.tool.name) != ''
        AND toString(attributes.gram.tool.name) NOT IN ('claude-code', 'codex', 'cursor')
        AND toString(attributes.gram.hook.event) IN ('PostToolUse', 'PostToolUseFailure')
    ) AS is_agent_tool_call
SELECT
    uniqExact(gram_project_id, toStartOfHour(fromUnixTimestamp64Nano(time_unix_nano, 'UTC')), chat_id) AS expected_rows,
    uniqExact(gram_project_id, chat_id) AS expected_chats,
    toStartOfHour(fromUnixTimestamp64Nano(min(time_unix_nano), 'UTC')) AS expected_oldest_bucket,
    toStartOfHour(fromUnixTimestamp64Nano(max(time_unix_nano), 'UTC')) AS expected_newest_bucket
FROM telemetry_logs
WHERE time_unix_nano >= chat_session_backfill_floor_unix_nano
  AND time_unix_nano < chat_session_cutoff_unix_nano
  AND chat_id != ''
  AND (is_claude_api_request OR is_claude_tool_result OR is_agent_usage_row OR is_agent_tool_call);

-- 0c. Expected measure totals from the source (same admitted row set), for
-- the step-2 sum comparison. These must match the summary-side totals to the
-- cent/token.
WITH
    toUnixTimestamp64Nano(toDateTime64('2026-07-21 22:00:00', 9, 'UTC')) AS chat_session_cutoff_unix_nano,
    toUnixTimestamp64Nano(toDateTime64('2026-04-24 00:00:00', 9, 'UTC')) AS chat_session_backfill_floor_unix_nano,
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
    (startsWith(gram_urn, 'codex:usage') OR startsWith(gram_urn, 'cursor:usage') OR startsWith(gram_urn, 'claude_chat:usage') OR startsWith(gram_urn, 'claude_chat:cost')) AS is_agent_usage_row,
    (
        hook_source IN ('codex', 'cursor')
        AND toString(attributes.gram.tool.name) != ''
        AND toString(attributes.gram.tool.name) NOT IN ('claude-code', 'codex', 'cursor')
        AND toString(attributes.gram.hook.event) IN ('PostToolUse', 'PostToolUseFailure')
    ) AS is_agent_tool_call,
    (is_claude_tool_result OR is_agent_tool_call) AS is_counted_tool_call,
    (is_claude_api_request OR is_agent_usage_row) AS is_usage_row,
    (
        (is_claude_tool_result AND toString(attributes.success) = 'false')
        OR (is_agent_tool_call AND (toString(attributes.gram.hook.event) = 'PostToolUseFailure' OR toInt32OrZero(toString(attributes.http.response.status_code)) >= 400))
    ) AS is_failed_tool_call
SELECT
    sumIf(if(is_claude_api_request, toInt64OrZero(toString(attributes.input_tokens)), toInt64OrZero(toString(attributes.gen_ai.usage.input_tokens))), is_usage_row) AS expected_input_tokens,
    sumIf(if(is_claude_api_request, toInt64OrZero(toString(attributes.output_tokens)), toInt64OrZero(toString(attributes.gen_ai.usage.output_tokens))), is_usage_row) AS expected_output_tokens,
    round(sumIf(if(is_claude_api_request, multiIf(toString(attributes.cost_usd) != '', toFloat64OrZero(toString(attributes.cost_usd)), toString(attributes.cost_usd_micros) != '', toFloat64OrZero(toString(attributes.cost_usd_micros)) / 1000000, 0), toFloat64OrZero(toString(attributes.gen_ai.usage.cost))), is_usage_row), 2) AS expected_cost,
    countIf(is_failed_tool_call) AS expected_failed_tool_calls
FROM telemetry_logs
WHERE time_unix_nano >= chat_session_backfill_floor_unix_nano
  AND time_unix_nano < chat_session_cutoff_unix_nano
  AND chat_id != ''
  AND (is_claude_api_request OR is_claude_tool_result OR is_agent_usage_row OR is_agent_tool_call);

-- ============================================================================
-- Step 1 — reset + backfill, always run together. The DELETE wipes
-- everything below the boundary first — a no-op on a first clean run, the
-- repair for partial MV buckets after a late deploy (step -1a), and the
-- recovery for a failed or duplicated earlier attempt — so the INSERT is
-- the range's only writer. The INSERT is a single pass over <= 90 days of
-- telemetry_logs; if it hits execution limits, split into disjoint time
-- slices by AND-ing ranges onto the WHERE clause (each range covered
-- exactly once), and add the slices' expected_rows from step 0b together.
-- ============================================================================

ALTER TABLE chat_session_summaries
    DELETE WHERE time_bucket < toDateTime('2026-07-21 22:00:00', 'UTC')
    SETTINGS mutations_sync = 2;

INSERT INTO chat_session_summaries (gram_project_id, time_bucket, chat_id, session_user_email, session_hook_source, session_model, start_time_unix_nano, end_time_unix_nano, message_count, tool_call_count, failed_tool_call_count, total_input_tokens, total_output_tokens, total_tokens, cache_read_input_tokens, cache_creation_input_tokens, total_cost, department_names, job_titles, employee_types, division_names, cost_center_names, emails, hostnames, models, hook_sources, account_types, providers, billing_modes, roles, groups, attribution_tuples)
WITH
    toUnixTimestamp64Nano(toDateTime64('2026-07-21 22:00:00', 9, 'UTC')) AS chat_session_cutoff_unix_nano,
    toUnixTimestamp64Nano(toDateTime64('2026-04-24 00:00:00', 9, 'UTC')) AS chat_session_backfill_floor_unix_nano,
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
    (startsWith(gram_urn, 'codex:usage') OR startsWith(gram_urn, 'cursor:usage') OR startsWith(gram_urn, 'claude_chat:usage') OR startsWith(gram_urn, 'claude_chat:cost')) AS is_agent_usage_row,
    (
        hook_source IN ('codex', 'cursor')
        AND toString(attributes.gram.tool.name) != ''
        AND toString(attributes.gram.tool.name) NOT IN ('claude-code', 'codex', 'cursor')
        AND toString(attributes.gram.hook.event) IN ('PostToolUse', 'PostToolUseFailure')
    ) AS is_agent_tool_call,
    (is_claude_tool_result OR is_agent_tool_call) AS is_counted_tool_call,
    (is_claude_api_request OR is_agent_usage_row) AS is_usage_row,
    (
        (is_claude_tool_result AND toString(attributes.success) = 'false')
        OR (is_agent_tool_call AND (toString(attributes.gram.hook.event) = 'PostToolUseFailure' OR toInt32OrZero(toString(attributes.http.response.status_code)) >= 400))
    ) AS is_failed_tool_call,
    multiIf(
        toString(attributes.tool_use_id) != '', toString(attributes.tool_use_id),
        toString(attributes.gen_ai.tool.call.id) != '', toString(attributes.gen_ai.tool.call.id),
        toString(id)
    ) AS tool_call_dedup_id,
    if(is_claude_api_request, toString(attributes.prompt.id), toString(attributes.gen_ai.response.id)) AS session_message_id,
    multiIf(
        is_claude_api_request AND toString(attributes.model) != '', toString(attributes.model),
        is_claude_api_request AND toString(attributes.gen_ai.request.model) != '', toString(attributes.gen_ai.request.model),
        toString(attributes.gen_ai.response.model)
    ) AS effective_model
SELECT
    gram_project_id,
    toStartOfHour(fromUnixTimestamp64Nano(time_unix_nano, 'UTC')) AS time_bucket,
    chat_id,
    max(user_email) AS session_user_email,
    max(hook_source) AS session_hook_source,
    argMaxIfState(effective_model, time_unix_nano, effective_model != '') AS session_model,
    min(time_unix_nano) AS start_time_unix_nano,
    max(time_unix_nano) AS end_time_unix_nano,
    uniqExactIfState(session_message_id, session_message_id != '') AS message_count,
    uniqExactIfState(tool_call_dedup_id, is_counted_tool_call) AS tool_call_count,
    countIf(is_failed_tool_call) AS failed_tool_call_count,
    sumIf(if(is_claude_api_request, toInt64OrZero(toString(attributes.input_tokens)), toInt64OrZero(toString(attributes.gen_ai.usage.input_tokens))), is_usage_row) AS total_input_tokens,
    sumIf(if(is_claude_api_request, toInt64OrZero(toString(attributes.output_tokens)), toInt64OrZero(toString(attributes.gen_ai.usage.output_tokens))), is_usage_row) AS total_output_tokens,
    sumIf(if(is_claude_api_request, toInt64OrZero(toString(attributes.input_tokens)) + toInt64OrZero(toString(attributes.output_tokens)) + toInt64OrZero(toString(attributes.cache_creation_tokens)), toInt64OrZero(toString(attributes.gen_ai.usage.input_tokens)) + toInt64OrZero(toString(attributes.gen_ai.usage.output_tokens)) + toInt64OrZero(toString(attributes.gen_ai.usage.cache_creation.input_tokens))), is_usage_row) AS total_tokens,
    sumIf(if(is_claude_api_request, toInt64OrZero(toString(attributes.cache_read_tokens)), toInt64OrZero(toString(attributes.gen_ai.usage.cache_read.input_tokens))), is_usage_row) AS cache_read_input_tokens,
    sumIf(if(is_claude_api_request, toInt64OrZero(toString(attributes.cache_creation_tokens)), toInt64OrZero(toString(attributes.gen_ai.usage.cache_creation.input_tokens))), is_usage_row) AS cache_creation_input_tokens,
    sumIf(if(is_claude_api_request, multiIf(toString(attributes.cost_usd) != '', toFloat64OrZero(toString(attributes.cost_usd)), toString(attributes.cost_usd_micros) != '', toFloat64OrZero(toString(attributes.cost_usd_micros)) / 1000000, 0), toFloat64OrZero(toString(attributes.gen_ai.usage.cost))), is_usage_row) AS total_cost,
    groupUniqArray(toString(attributes.user.attributes.department_name)) AS department_names,
    groupUniqArray(toString(attributes.user.attributes.job_title)) AS job_titles,
    groupUniqArray(toString(attributes.user.attributes.employee_type)) AS employee_types,
    groupUniqArray(toString(attributes.user.attributes.division_name)) AS division_names,
    groupUniqArray(toString(attributes.user.attributes.cost_center_name)) AS cost_center_names,
    groupUniqArray(if(user_email != '', user_email, toString(attributes.gram.hook.hostname))) AS emails,
    groupUniqArray(toString(attributes.gram.hook.hostname)) AS hostnames,
    groupUniqArray(effective_model) AS models,
    groupUniqArray(hook_source) AS hook_sources,
    groupUniqArray(account_type) AS account_types,
    groupUniqArray(provider) AS providers,
    groupUniqArray(billing_mode) AS billing_modes,
    groupUniqArrayArray(arraySort(JSONExtract(ifNull(toJSONString(attributes.user.roles), '[]'), 'Array(String)'))) AS roles,
    groupUniqArrayArray(arraySort(JSONExtract(ifNull(toJSONString(attributes.user.groups), '[]'), 'Array(String)'))) AS groups,
    groupUniqArrayIf(tuple(toString(attributes.query_source), toString(attributes.skill.name), toString(attributes.agent.name), toString(attributes.mcp_server.name), toString(attributes.mcp_tool.name)), is_claude_api_request) AS attribution_tuples
FROM telemetry_logs
WHERE time_unix_nano >= chat_session_backfill_floor_unix_nano
  AND time_unix_nano < chat_session_cutoff_unix_nano
  AND chat_id != ''
  AND (is_claude_api_request OR is_claude_tool_result OR is_agent_usage_row OR is_agent_tool_call)
GROUP BY gram_project_id, time_bucket, chat_id;

-- ============================================================================
-- Step 2 — verify the insert against the step-0 expectations.
-- ============================================================================

-- 2a. Row counts and bucket range. Checks:
--   * pre_cutoff_rows = expected_rows from step 0b (was 0 before step 1);
--   * pre_cutoff_chats = expected_chats from step 0b;
--   * oldest/newest pre-cutoff bucket match expected_oldest/newest_bucket
--     (newest must be < the cutoff — the boundary held);
--   * post_cutoff_rows >= its step-0a value (live MV kept writing; it must
--     not have shrunk or been touched by this insert).
SELECT
    countIf(time_bucket < toDateTime('2026-07-21 22:00:00', 'UTC')) AS pre_cutoff_rows,
    uniqExactIf((gram_project_id, chat_id), time_bucket < toDateTime('2026-07-21 22:00:00', 'UTC')) AS pre_cutoff_chats,
    minIf(time_bucket, time_bucket < toDateTime('2026-07-21 22:00:00', 'UTC')) AS pre_cutoff_oldest,
    maxIf(time_bucket, time_bucket < toDateTime('2026-07-21 22:00:00', 'UTC')) AS pre_cutoff_newest,
    countIf(time_bucket >= toDateTime('2026-07-21 22:00:00', 'UTC')) AS post_cutoff_rows
FROM chat_session_summaries;

-- 2b. Measure totals over the backfilled range must equal step 0c exactly
-- (same rows, same expressions — any drift means a predicate mismatch).
SELECT
    sum(total_input_tokens) AS actual_input_tokens,
    sum(total_output_tokens) AS actual_output_tokens,
    round(sum(total_cost), 2) AS actual_cost,
    sum(failed_tool_call_count) AS actual_failed_tool_calls
FROM chat_session_summaries
WHERE time_bucket < toDateTime('2026-07-21 22:00:00', 'UTC');

-- 2c. Per-day cost spot-check across the backfilled range: eyeball for holes
-- (missing days) or spikes before cutting the read path over.
SELECT toStartOfDay(time_bucket) AS day,
       count() AS bucket_rows,
       round(sum(total_cost), 2) AS cost
FROM chat_session_summaries
WHERE time_bucket < toDateTime('2026-07-21 22:00:00', 'UTC')
GROUP BY day
ORDER BY day DESC;

-- ============================================================================
-- RECOVERY — partial failure or accidental double run: re-run step 1 as a
-- unit (its reset DELETE clears the boundary-bounded range this runbook
-- owns; the MV never writes below the boundary), then re-verify with step 2.
-- ============================================================================
