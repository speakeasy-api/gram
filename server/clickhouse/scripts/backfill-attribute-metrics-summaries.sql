-- Backfill for attribute_metrics_summaries.
--
-- attribute_metrics_summaries_mv was created without POPULATE, so it only
-- captured rows inserted after it existed. This rebuilds the aggregate from
-- telemetry_logs (the source of truth, 30-day TTL — so 30 days is the max
-- recoverable window) by replaying the MV's SELECT verbatim.
--
-- The INSERT must emit the SAME *State() aggregate combinators as the MV so the
-- rows are valid AggregateFunction states for the AggregatingMergeTree.
--
-- Run manually against the target ClickHouse (NOT wired into any migration or
-- deploy path). TRUNCATE first so a partially-populated aggregate cannot
-- double-count; the table is fully rebuildable from telemetry_logs, so nothing
-- is lost. Prefer a low-traffic window: rows inserted between the TRUNCATE and
-- the INSERT snapshot can be counted twice (once by the live MV, once here).

TRUNCATE TABLE attribute_metrics_summaries;

INSERT INTO attribute_metrics_summaries
SELECT
    gram_project_id,
    toStartOfHour(fromUnixTimestamp64Nano(time_unix_nano)) AS time_bucket,

    -- User-identity dimensions
    toString(attributes.user.attributes.department_name) AS department_name,
    toString(attributes.user.attributes.job_title) AS job_title,
    toString(attributes.user.attributes.employee_type) AS employee_type,
    toString(attributes.user.attributes.division_name) AS division_name,
    toString(attributes.user.attributes.cost_center_name) AS cost_center_name,
    user_email AS user_email,

    -- Request dimensions
    toString(attributes.gen_ai.response.model) AS model,
    hook_source,

    -- Multi-valued dimensions extracted tolerantly from JSON/Dynamic values.
    arraySort(JSONExtract(ifNull(toJSONString(attributes.user.roles), '[]'), 'Array(String)')) AS roles,
    arraySort(JSONExtract(ifNull(toJSONString(attributes.user.groups), '[]'), 'Array(String)')) AS groups,

    -- Cardinality
    uniqExactIfState(toString(attributes.gen_ai.conversation.id), toString(attributes.gen_ai.conversation.id) != '') AS total_chats,

    -- Token sums
    sumIfState(toInt64OrZero(toString(attributes.gen_ai.usage.input_tokens)), toString(attributes.gen_ai.usage.input_tokens) != '') AS total_input_tokens,
    sumIfState(toInt64OrZero(toString(attributes.gen_ai.usage.output_tokens)), toString(attributes.gen_ai.usage.output_tokens) != '') AS total_output_tokens,
    sumIfState(toInt64OrZero(toString(attributes.gen_ai.usage.total_tokens)), toString(attributes.gen_ai.usage.total_tokens) != '') AS total_tokens,
    sumIfState(toInt64OrZero(toString(attributes.gen_ai.usage.cache_read.input_tokens)), toString(attributes.gen_ai.usage.cache_read.input_tokens) != '') AS cache_read_input_tokens,
    sumIfState(toInt64OrZero(toString(attributes.gen_ai.usage.cache_creation.input_tokens)), toString(attributes.gen_ai.usage.cache_creation.input_tokens) != '') AS cache_creation_input_tokens,

    -- Cost
    sumIfState(toFloat64OrZero(toString(attributes.gen_ai.usage.cost)), toString(attributes.gen_ai.usage.cost) != '') AS total_cost,

    -- Tool call count
    countIfState(startsWith(toString(attributes.gram.tool.urn), 'tools:')) AS total_tool_calls
FROM telemetry_logs
-- Only the recoverable window; telemetry_logs TTL already bounds it to 30 days,
-- but the explicit predicate keeps the scan tight and the intent clear.
WHERE time_unix_nano >= toInt64(toUnixTimestamp(now() - INTERVAL 30 DAY)) * 1000000000
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
    groups;
