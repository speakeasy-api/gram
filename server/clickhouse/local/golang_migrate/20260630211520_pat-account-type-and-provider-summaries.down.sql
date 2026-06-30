-- reverse: create "trace_summaries_mv" view
DROP VIEW `trace_summaries_mv`;
-- reverse: drop "trace_summaries_mv" view
CREATE MATERIALIZED VIEW `trace_summaries_mv` TO `trace_summaries` AS SELECT trace_id, gram_project_id, any(gram_deployment_id) AS gram_deployment_id, any(gram_function_id) AS gram_function_id, any(gram_urn) AS gram_urn, any(tool_name) AS tool_name, any(tool_source) AS tool_source, any(event_source) AS event_source, any(user_email) AS user_email, any(hook_source) AS hook_source, any(skill_name) AS skill_name, anyIf(toolset_slug, toolset_slug != '') AS toolset_slug, anyIf(external_user_id, external_user_id != '') AS external_user_id, anyIf(user_id, user_id != '') AS user_id, anyIf(toString(attributes.gram.mcp.match), toString(attributes.gram.mcp.match) != '') AS mcp_match, anyIf(toString(attributes.gram.mcp.server_url), toString(attributes.gram.mcp.server_url) != '') AS mcp_server_url, min(time_unix_nano) AS start_time_unix_nano, toUInt64(count(*)) AS log_count, anyIfState(toInt32OrNull(toString(attributes.http.response.status_code)), toString(attributes.http.response.status_code) != '') AS http_status_code, max(if(toString(attributes.gen_ai.tool.call.result) != '', 1, 0)) AS has_result, max(if(toString(attributes.gram.hook.error) != '', 1, 0)) AS has_error, max(if(toString(attributes.gram.hook.block_reason) != '', 1, 0)) AS has_block, anyIf(toString(attributes.gram.hook.block_reason), toString(attributes.gram.hook.block_reason) != '') AS block_reason FROM telemetry_logs WHERE (trace_id IS NOT NULL) AND (trace_id != '') AND (NOT startsWith(telemetry_logs.gram_urn, 'urn:uuid:')) GROUP BY trace_id, gram_project_id;
-- reverse: create "attribute_metrics_summaries_mv" view
DROP VIEW `attribute_metrics_summaries_mv`;
-- reverse: drop "attribute_metrics_summaries_mv" view
CREATE MATERIALIZED VIEW `attribute_metrics_summaries_mv` TO `attribute_metrics_summaries` AS WITH toUnixTimestamp64Nano(toDateTime64('2026-06-20 00:00:00', 9, 'UTC')) AS attribute_metrics_cutoff_unix_nano SELECT gram_project_id, toStartOfHour(fromUnixTimestamp64Nano(time_unix_nano)) AS time_bucket, toString(attributes.user.attributes.department_name) AS department_name, toString(attributes.user.attributes.job_title) AS job_title, toString(attributes.user.attributes.employee_type) AS employee_type, toString(attributes.user.attributes.division_name) AS division_name, toString(attributes.user.attributes.cost_center_name) AS cost_center_name, user_email AS user_email, toString(attributes.gen_ai.response.model) AS model, hook_source, arraySort(JSONExtract(ifNull(toJSONString(attributes.user.roles), '[]'), 'Array(String)')) AS roles, arraySort(JSONExtract(ifNull(toJSONString(attributes.user.groups), '[]'), 'Array(String)')) AS groups, uniqExactIfState(toString(attributes.gen_ai.conversation.id), toString(attributes.gen_ai.conversation.id) != '') AS total_chats, sumIfState(toInt64OrZero(toString(attributes.gen_ai.usage.input_tokens)), toString(attributes.gen_ai.usage.input_tokens) != '') AS total_input_tokens, sumIfState(toInt64OrZero(toString(attributes.gen_ai.usage.output_tokens)), toString(attributes.gen_ai.usage.output_tokens) != '') AS total_output_tokens, sumIfState(toInt64OrZero(toString(attributes.gen_ai.usage.total_tokens)), toString(attributes.gen_ai.usage.total_tokens) != '') AS total_tokens, sumIfState(toInt64OrZero(toString(attributes.gen_ai.usage.cache_read.input_tokens)), toString(attributes.gen_ai.usage.cache_read.input_tokens) != '') AS cache_read_input_tokens, sumIfState(toInt64OrZero(toString(attributes.gen_ai.usage.cache_creation.input_tokens)), toString(attributes.gen_ai.usage.cache_creation.input_tokens) != '') AS cache_creation_input_tokens, sumIfState(toFloat64OrZero(toString(attributes.gen_ai.usage.cost)), toString(attributes.gen_ai.usage.cost) != '') AS total_cost, countIfState((toString(attributes.gram.tool.name) != '') AND (toString(attributes.gram.tool.name) NOT IN ('claude-code', 'codex', 'cursor')) AND (toString(attributes.gram.hook.event) IN ('PostToolUse', 'PostToolUseFailure'))) AS total_tool_calls FROM telemetry_logs WHERE (time_unix_nano >= attribute_metrics_cutoff_unix_nano) AND (startsWith(gram_urn, 'claude-code:usage') OR startsWith(gram_urn, 'codex:usage') OR startsWith(gram_urn, 'cursor:usage') OR ((toString(attributes.gen_ai.operation.name) = 'chat') AND (toString(attributes.gen_ai.usage.cost) != '')) OR ((toString(attributes.gram.tool.name) != '') AND (toString(attributes.gram.tool.name) NOT IN ('claude-code', 'codex', 'cursor')))) GROUP BY gram_project_id, time_bucket, department_name, job_title, employee_type, division_name, cost_center_name, user_email, model, hook_source, roles, groups;
ALTER TABLE `trace_summaries` DROP COLUMN `provider`;
ALTER TABLE `trace_summaries` DROP COLUMN `account_type`;
-- reverse: recreate "attribute_metrics_summaries" WITHOUT account_type/provider.
-- A sort-key extension (MODIFY ORDER BY) cannot be reversed via ALTER (it only
-- extends the key), so the only way back to the original 12-dimension table is
-- drop + recreate. This wipes aggregated history on rollback.
DROP TABLE `attribute_metrics_summaries`;
CREATE TABLE `attribute_metrics_summaries` (
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
  `total_tool_calls` AggregateFunction(countIf, UInt8)
) ENGINE = AggregatingMergeTree
ORDER BY (`gram_project_id`, `time_bucket`, `department_name`, `job_title`, `employee_type`, `division_name`, `cost_center_name`, `user_email`, `model`, `hook_source`, `roles`, `groups`) TTL time_bucket + toIntervalDay(30) SETTINGS index_granularity = 8192 COMMENT 'Pre-aggregated cost/token/usage metrics broken down by user-identity and request dimensions, powering the generic telemetry.query analytics endpoint.';
