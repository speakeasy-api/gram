-- Create long-lived Claude Chat event storage. ReplacingMergeTree retains the
-- retry key while FINAL applies it only when billing/analytics reads occur.
CREATE TABLE `claude_chat_metrics` (
  `gram_project_id` UUID,
  `event_hash` String,
  `time_bucket` DateTime('UTC'),
  `observed_time_unix_nano` Int64,
  `department_name` String,
  `job_title` String,
  `employee_type` String,
  `division_name` String,
  `cost_center_name` String,
  `user_email` String,
  `model` String,
  `hook_source` String,
  `roles` Array(String),
  `groups` Array(String),
  `total_input_tokens` Int64,
  `total_output_tokens` Int64,
  `total_tokens` Int64,
  `cache_read_input_tokens` Int64,
  `cache_creation_input_tokens` Int64,
  `total_cost` Float64,
  `account_type` String,
  `provider` String,
  `billing_mode` String,
  `query_source` String,
  `skill_name` String,
  `agent_name` String,
  `mcp_server_name` String,
  `mcp_tool_name` String
) ENGINE = ReplacingMergeTree(observed_time_unix_nano)
PRIMARY KEY (`gram_project_id`, `event_hash`)
ORDER BY (`gram_project_id`, `event_hash`)
PARTITION BY (toYYYYMM(time_bucket))
TTL time_bucket + toIntervalDay(730)
SETTINGS index_granularity = 8192
COMMENT 'Long-lived Claude Chat usage and cost events keyed for read-time retry deduplication.';

-- Remove Claude Chat from the row-summing attribute aggregate without
-- interrupting ingestion. The dedicated read model below replaces this path.
ALTER TABLE `attribute_metrics_summaries_mv` MODIFY QUERY
WITH toUnixTimestamp64Nano(toDateTime64('2026-07-14 00:00:00', 9, 'UTC')) AS attribute_metrics_cutoff_unix_nano, gram_urn = 'claude-code:otel:logs' AS is_claude_otel_row, is_claude_otel_row AND (chat_id != '') AND (toString(attributes.prompt.id) != '') AND ((toString(attributes.event.name) = 'api_request') OR (body = 'claude_code.api_request')) AS is_claude_api_request, is_claude_otel_row AND ((toString(attributes.event.name) = 'tool_result') OR (body = 'claude_code.tool_result')) AS is_claude_tool_result, startsWith(gram_urn, 'codex:usage') OR startsWith(gram_urn, 'cursor:usage') AS is_agent_usage_row, (toString(attributes.gram.hook.source) IN ('codex', 'cursor')) AND (toString(attributes.gram.tool.name) != '') AND (toString(attributes.gram.tool.name) NOT IN ('claude-code', 'codex', 'cursor')) AND (toString(attributes.gram.hook.event) IN ('PostToolUse', 'PostToolUseFailure')) AS is_agent_tool_call, is_claude_tool_result OR is_agent_tool_call AS is_counted_tool_call, multiIf(toString(attributes.tool_use_id) != '', toString(attributes.tool_use_id), toString(attributes.gen_ai.tool.call.id) != '', toString(attributes.gen_ai.tool.call.id), toString(id)) AS tool_call_dedup_id SELECT gram_project_id, toStartOfHour(fromUnixTimestamp64Nano(time_unix_nano)) AS time_bucket, toString(attributes.user.attributes.department_name) AS department_name, toString(attributes.user.attributes.job_title) AS job_title, toString(attributes.user.attributes.employee_type) AS employee_type, toString(attributes.user.attributes.division_name) AS division_name, toString(attributes.user.attributes.cost_center_name) AS cost_center_name, user_email AS user_email, multiIf(is_claude_api_request AND (toString(attributes.model) != ''), toString(attributes.model), is_claude_api_request AND (toString(attributes.gen_ai.request.model) != ''), toString(attributes.gen_ai.request.model), toString(attributes.gen_ai.response.model)) AS model, hook_source, arraySort(JSONExtract(ifNull(toJSONString(attributes.user.roles), '[]'), 'Array(String)')) AS roles, arraySort(JSONExtract(ifNull(toJSONString(attributes.user.groups), '[]'), 'Array(String)')) AS groups, uniqExactIfState(toString(attributes.gen_ai.conversation.id), (toString(attributes.gen_ai.conversation.id) != '') AND (is_claude_api_request OR is_agent_usage_row)) AS total_chats, sumIfState(if(is_claude_api_request, toInt64OrZero(toString(attributes.input_tokens)), toInt64OrZero(toString(attributes.gen_ai.usage.input_tokens))), is_claude_api_request OR is_agent_usage_row) AS total_input_tokens, sumIfState(if(is_claude_api_request, toInt64OrZero(toString(attributes.output_tokens)), toInt64OrZero(toString(attributes.gen_ai.usage.output_tokens))), is_claude_api_request OR is_agent_usage_row) AS total_output_tokens, sumIfState(if(is_claude_api_request, (toInt64OrZero(toString(attributes.input_tokens)) + toInt64OrZero(toString(attributes.output_tokens))) + toInt64OrZero(toString(attributes.cache_creation_tokens)), (toInt64OrZero(toString(attributes.gen_ai.usage.input_tokens)) + toInt64OrZero(toString(attributes.gen_ai.usage.output_tokens))) + toInt64OrZero(toString(attributes.gen_ai.usage.cache_creation.input_tokens))), is_claude_api_request OR is_agent_usage_row) AS total_tokens, sumIfState(if(is_claude_api_request, toInt64OrZero(toString(attributes.cache_read_tokens)), toInt64OrZero(toString(attributes.gen_ai.usage.cache_read.input_tokens))), is_claude_api_request OR is_agent_usage_row) AS cache_read_input_tokens, sumIfState(if(is_claude_api_request, toInt64OrZero(toString(attributes.cache_creation_tokens)), toInt64OrZero(toString(attributes.gen_ai.usage.cache_creation.input_tokens))), is_claude_api_request OR is_agent_usage_row) AS cache_creation_input_tokens, sumIfState(if(is_claude_api_request, multiIf(toString(attributes.cost_usd) != '', toFloat64OrZero(toString(attributes.cost_usd)), toString(attributes.cost_usd_micros) != '', toFloat64OrZero(toString(attributes.cost_usd_micros)) / 1000000, 0), toFloat64OrZero(toString(attributes.gen_ai.usage.cost))), is_claude_api_request OR is_agent_usage_row) AS total_cost, countIfState(is_counted_tool_call) AS total_tool_calls, uniqExactIfState(tool_call_dedup_id, is_counted_tool_call) AS unique_tool_calls, account_type, provider, billing_mode, if(is_claude_api_request, toString(attributes.query_source), '') AS query_source, if(is_claude_api_request, toString(attributes.skill.name), '') AS skill_name, if(is_claude_api_request, toString(attributes.agent.name), '') AS agent_name, if(is_claude_api_request, toString(attributes.mcp_server.name), '') AS mcp_server_name, if(is_claude_api_request, toString(attributes.mcp_tool.name), '') AS mcp_tool_name, toUInt8(0) AS generation, toUInt8(1) AS is_active FROM telemetry_logs WHERE (time_unix_nano >= attribute_metrics_cutoff_unix_nano) AND (is_claude_api_request OR is_claude_tool_result OR is_agent_usage_row OR is_agent_tool_call) GROUP BY gram_project_id, time_bucket, department_name, job_title, employee_type, division_name, cost_center_name, user_email, model, hook_source, roles, groups, account_type, provider, billing_mode, query_source, skill_name, agent_name, mcp_server_name, mcp_tool_name;

-- Keep Claude Chat rows out of the legacy row-summing aggregate. MODIFY QUERY
-- swaps the definition without a drop/recreate ingestion gap.
ALTER TABLE `metrics_summaries_mv` MODIFY QUERY
SELECT
    gram_project_id,
    toStartOfHour(fromUnixTimestamp64Nano(time_unix_nano)) AS time_bucket,
    min(time_unix_nano) AS first_seen_unix_nano,
    max(time_unix_nano) AS last_seen_unix_nano,
    uniqExactIfState(toString(attributes.gen_ai.conversation.id), toString(attributes.gen_ai.conversation.id) != '') AS total_chats,
    uniqExactIfState(toString(attributes.gen_ai.response.model), toString(attributes.gen_ai.response.model) != '') AS distinct_models,
    uniqExactIfState(toString(attributes.gen_ai.provider.name), toString(attributes.gen_ai.provider.name) != '') AS distinct_providers,
    sumIfState(toInt64OrZero(toString(attributes.gen_ai.usage.input_tokens)), toString(attributes.gen_ai.usage.input_tokens) != '') AS total_input_tokens,
    sumIfState(toInt64OrZero(toString(attributes.gen_ai.usage.output_tokens)), toString(attributes.gen_ai.usage.output_tokens) != '') AS total_output_tokens,
    sumIfState(toInt64OrZero(toString(attributes.gen_ai.usage.total_tokens)), toString(attributes.gen_ai.usage.total_tokens) != '') AS total_tokens,
    sumIfState(toInt64OrZero(toString(attributes.gen_ai.usage.cache_read.input_tokens)), toString(attributes.gen_ai.usage.cache_read.input_tokens) != '') AS cache_read_input_tokens,
    sumIfState(toInt64OrZero(toString(attributes.gen_ai.usage.cache_creation.input_tokens)), toString(attributes.gen_ai.usage.cache_creation.input_tokens) != '') AS cache_creation_input_tokens,
    sumIfState(toFloat64OrZero(toString(attributes.gen_ai.usage.cost)), toString(attributes.gen_ai.usage.cost) != '') AS total_cost,
    avgIfState(toFloat64OrZero(toString(attributes.gen_ai.usage.total_tokens)), toString(attributes.gen_ai.conversation.id) != '') AS avg_tokens_per_request,
    countIfState(toString(attributes.gen_ai.conversation.id) != '') AS total_chat_requests,
    avgIfState(toFloat64OrZero(toString(attributes.gen_ai.conversation.duration)) * 1000, toString(attributes.gen_ai.conversation.id) != '') AS avg_chat_duration_ms,
    countIfState(position(toString(attributes.gen_ai.response.finish_reasons), 'stop') > 0) AS finish_reason_stop,
    countIfState(position(toString(attributes.gen_ai.response.finish_reasons), 'tool_calls') > 0) AS finish_reason_tool_calls,
    countIfState(startsWith(toString(attributes.gram.tool.urn), 'tools:')) AS total_tool_calls,
    countIfState(startsWith(toString(attributes.gram.tool.urn), 'tools:') AND toInt32OrZero(toString(attributes.http.response.status_code)) >= 200 AND toInt32OrZero(toString(attributes.http.response.status_code)) < 300) AS tool_call_success,
    countIfState(startsWith(toString(attributes.gram.tool.urn), 'tools:') AND toInt32OrZero(toString(attributes.http.response.status_code)) >= 400) AS tool_call_failure,
    avgIfState(toFloat64OrZero(toString(attributes.http.server.request.duration)) * 1000, startsWith(toString(attributes.gram.tool.urn), 'tools:')) AS avg_tool_duration_ms,
    countIfState(evaluation_score_label = 'success') AS chat_resolution_success,
    countIfState(evaluation_score_label = 'failure') AS chat_resolution_failure,
    countIfState(evaluation_score_label = 'partial') AS chat_resolution_partial,
    countIfState(evaluation_score_label = 'abandoned') AS chat_resolution_abandoned,
    avgIfState(toFloat64OrZero(toString(attributes.gen_ai.evaluation.score.value)), evaluation_score_label != '') AS avg_chat_resolution_score,
    uniqExactIfState(toString(attributes.gen_ai.conversation.id), toString(attributes.gen_ai.conversation.id) != '' AND evaluation_score_label != '') AS evaluated_chats,
    uniqExactIfState(toString(attributes.gen_ai.conversation.id), toString(attributes.gen_ai.conversation.id) != '' AND evaluation_score_label = 'success') AS resolved_chats,
    uniqExactIfState(toString(attributes.gen_ai.conversation.id), toString(attributes.gen_ai.conversation.id) != '' AND evaluation_score_label = 'failure') AS failed_chats,
    avgIfState(toFloat64OrZero(toString(attributes.gen_ai.conversation.duration)) * 1000, evaluation_score_label = 'success') AS avg_resolution_time_ms,
    sumMapIfState(map(toString(attributes.gen_ai.response.model), toUInt64(1)), toString(attributes.gen_ai.conversation.id) != '' AND toString(attributes.gen_ai.response.model) != '') AS models,
    sumMapIfState(map(gram_urn, toUInt64(1)), startsWith(gram_urn, 'tools:')) AS tool_counts,
    sumMapIfState(map(gram_urn, toUInt64(1)), startsWith(gram_urn, 'tools:') AND toInt32OrZero(toString(attributes.http.response.status_code)) >= 200 AND toInt32OrZero(toString(attributes.http.response.status_code)) < 300) AS tool_success_counts,
    sumMapIfState(map(gram_urn, toUInt64(1)), startsWith(gram_urn, 'tools:') AND toInt32OrZero(toString(attributes.http.response.status_code)) >= 400) AS tool_failure_counts
FROM telemetry_logs
WHERE NOT startsWith(gram_urn, 'claude_chat:')
GROUP BY gram_project_id, time_bucket;

CREATE MATERIALIZED VIEW `claude_chat_metrics_mv` TO `claude_chat_metrics` AS
SELECT
    gram_project_id,
    toString(attributes.claude_chat.event_hash) AS event_hash,
    toStartOfHour(fromUnixTimestamp64Nano(time_unix_nano)) AS time_bucket,
    observed_time_unix_nano,
    toString(attributes.user.attributes.department_name) AS department_name,
    toString(attributes.user.attributes.job_title) AS job_title,
    toString(attributes.user.attributes.employee_type) AS employee_type,
    toString(attributes.user.attributes.division_name) AS division_name,
    toString(attributes.user.attributes.cost_center_name) AS cost_center_name,
    user_email,
    toString(attributes.gen_ai.response.model) AS model,
    hook_source,
    arraySort(JSONExtract(ifNull(toJSONString(attributes.user.roles), '[]'), 'Array(String)')) AS roles,
    arraySort(JSONExtract(ifNull(toJSONString(attributes.user.groups), '[]'), 'Array(String)')) AS groups,
    toInt64OrZero(toString(attributes.gen_ai.usage.input_tokens)) AS total_input_tokens,
    toInt64OrZero(toString(attributes.gen_ai.usage.output_tokens)) AS total_output_tokens,
    toInt64OrZero(toString(attributes.gen_ai.usage.input_tokens))
        + toInt64OrZero(toString(attributes.gen_ai.usage.output_tokens))
        + toInt64OrZero(toString(attributes.gen_ai.usage.cache_creation.input_tokens)) AS total_tokens,
    toInt64OrZero(toString(attributes.gen_ai.usage.cache_read.input_tokens)) AS cache_read_input_tokens,
    toInt64OrZero(toString(attributes.gen_ai.usage.cache_creation.input_tokens)) AS cache_creation_input_tokens,
    toFloat64OrZero(toString(attributes.gen_ai.usage.cost)) AS total_cost,
    account_type,
    provider,
    billing_mode,
    '' AS query_source,
    '' AS skill_name,
    '' AS agent_name,
    '' AS mcp_server_name,
    '' AS mcp_tool_name
FROM telemetry_logs
WHERE gram_urn IN ('claude_chat:usage:metrics', 'claude_chat:cost:metrics')
  AND toString(attributes.claude_chat.event_hash) != '';

CREATE VIEW `deduplicated_attribute_metrics_summaries` (
  `gram_project_id` UUID,
  `time_bucket` DateTime('UTC'),
  `department_name` String,
  `job_title` String,
  `employee_type` String,
  `division_name` String,
  `cost_center_name` String,
  `user_email` String,
  `model` String,
  `hook_source` String,
  `roles` Array(String),
  `groups` Array(String),
  `total_chats` AggregateFunction(uniqExactIf, String, UInt8),
  `total_input_tokens` AggregateFunction(sumIf, Int64, UInt8),
  `total_output_tokens` AggregateFunction(sumIf, Int64, UInt8),
  `total_tokens` AggregateFunction(sumIf, Int64, UInt8),
  `cache_read_input_tokens` AggregateFunction(sumIf, Int64, UInt8),
  `cache_creation_input_tokens` AggregateFunction(sumIf, Int64, UInt8),
  `total_cost` AggregateFunction(sumIf, Float64, UInt8),
  `total_tool_calls` AggregateFunction(countIf, UInt8),
  `unique_tool_calls` AggregateFunction(uniqExactIf, String, UInt8),
  `account_type` String,
  `provider` String,
  `billing_mode` String,
  `query_source` String,
  `skill_name` String,
  `agent_name` String,
  `mcp_server_name` String,
  `mcp_tool_name` String
) AS
SELECT
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
    total_chats,
    total_input_tokens,
    total_output_tokens,
    total_tokens,
    cache_read_input_tokens,
    cache_creation_input_tokens,
    total_cost,
    total_tool_calls,
    unique_tool_calls,
    account_type,
    provider,
    billing_mode,
    query_source,
    skill_name,
    agent_name,
    mcp_server_name,
    mcp_tool_name
FROM attribute_metrics_summaries
WHERE is_active = 1
UNION ALL
SELECT
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
    uniqExactIfState(toString(''), toUInt8(0)) AS total_chats,
    sumIfState(total_input_tokens, toUInt8(1)) AS total_input_tokens,
    sumIfState(total_output_tokens, toUInt8(1)) AS total_output_tokens,
    sumIfState(total_tokens, toUInt8(1)) AS total_tokens,
    sumIfState(cache_read_input_tokens, toUInt8(1)) AS cache_read_input_tokens,
    sumIfState(cache_creation_input_tokens, toUInt8(1)) AS cache_creation_input_tokens,
    sumIfState(total_cost, toUInt8(1)) AS total_cost,
    countIfState(toUInt8(0)) AS total_tool_calls,
    uniqExactIfState(toString(''), toUInt8(0)) AS unique_tool_calls,
    account_type,
    provider,
    billing_mode,
    query_source,
    skill_name,
    agent_name,
    mcp_server_name,
    mcp_tool_name
FROM claude_chat_metrics FINAL
GROUP BY
    gram_project_id,
    event_hash,
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
