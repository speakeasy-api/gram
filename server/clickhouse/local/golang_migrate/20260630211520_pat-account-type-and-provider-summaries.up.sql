-- Hand-edited (not atlas's DROP+recreate): add account/provider and Claude
-- attribution as sort-key dimensions to "attribute_metrics_summaries" NON-destructively.
-- ADD COLUMN and MODIFY ORDER BY must be one ALTER statement — ClickHouse only
-- lets MODIFY ORDER BY extend the key with columns added in the same ALTER.
-- Existing aggregates are preserved (no DROP TABLE).
ALTER TABLE `attribute_metrics_summaries`
  ADD COLUMN `account_type` String,
  ADD COLUMN `provider` String,
  ADD COLUMN `query_source` String,
  ADD COLUMN `skill_name` String,
  ADD COLUMN `agent_name` String,
  ADD COLUMN `mcp_server_name` String,
  ADD COLUMN `mcp_tool_name` String,
  MODIFY ORDER BY (`gram_project_id`, `time_bucket`, `department_name`, `job_title`, `employee_type`, `division_name`, `cost_center_name`, `user_email`, `model`, `hook_source`, `roles`, `groups`, `account_type`, `provider`, `query_source`, `skill_name`, `agent_name`, `mcp_server_name`, `mcp_tool_name`);
ALTER TABLE `trace_summaries` ADD COLUMN `account_type` SimpleAggregateFunction(max, String);
ALTER TABLE `trace_summaries` ADD COLUMN `provider` SimpleAggregateFunction(max, String);
-- drop "attribute_metrics_summaries_mv" view
DROP VIEW `attribute_metrics_summaries_mv`;
-- create "attribute_metrics_summaries_mv" view
CREATE MATERIALIZED VIEW `attribute_metrics_summaries_mv` TO `attribute_metrics_summaries` AS
WITH
  toUnixTimestamp64Nano(toDateTime64('2026-06-20 00:00:00', 9, 'UTC')) AS attribute_metrics_cutoff_unix_nano,
  ((chat_id != '') AND (toString(attributes.prompt.id) != '') AND ((toString(attributes.event.name) = 'api_request') OR (body = 'claude_code.api_request')) AND ((service_name = 'claude-code') OR (toString(resource_attributes.service.name) = 'claude-code') OR startsWith(body, 'claude_code.'))) AS is_claude_api_request,
  (startsWith(gram_urn, 'codex:usage') OR startsWith(gram_urn, 'cursor:usage') OR ((toString(attributes.gen_ai.operation.name) = 'chat') AND (toString(attributes.gen_ai.usage.cost) != '') AND NOT is_claude_api_request AND NOT startsWith(gram_urn, 'claude-code:usage'))) AS is_generic_usage_row,
  ((toString(attributes.gram.tool.name) != '') AND (toString(attributes.gram.tool.name) NOT IN ('claude-code', 'codex', 'cursor'))) AS is_tool_row,
  (is_tool_row AND (toString(attributes.gram.hook.event) IN ('PostToolUse', 'PostToolUseFailure'))) AS is_completed_tool_call
SELECT
  gram_project_id,
  toStartOfHour(fromUnixTimestamp64Nano(time_unix_nano)) AS time_bucket,
  toString(attributes.user.attributes.department_name) AS department_name,
  toString(attributes.user.attributes.job_title) AS job_title,
  toString(attributes.user.attributes.employee_type) AS employee_type,
  toString(attributes.user.attributes.division_name) AS division_name,
  toString(attributes.user.attributes.cost_center_name) AS cost_center_name,
  user_email AS user_email,
  multiIf(is_claude_api_request AND toString(attributes.model) != '', toString(attributes.model), is_claude_api_request AND toString(attributes.gen_ai.request.model) != '', toString(attributes.gen_ai.request.model), toString(attributes.gen_ai.response.model)) AS model,
  hook_source,
  arraySort(JSONExtract(ifNull(toJSONString(attributes.user.roles), '[]'), 'Array(String)')) AS roles,
  arraySort(JSONExtract(ifNull(toJSONString(attributes.user.groups), '[]'), 'Array(String)')) AS groups,
  uniqExactIfState(toString(attributes.gen_ai.conversation.id), (toString(attributes.gen_ai.conversation.id) != '') AND (is_claude_api_request OR is_generic_usage_row)) AS total_chats,
  sumIfState(if(is_claude_api_request, toInt64OrZero(toString(attributes.input_tokens)), toInt64OrZero(toString(attributes.gen_ai.usage.input_tokens))), is_claude_api_request OR is_generic_usage_row) AS total_input_tokens,
  sumIfState(if(is_claude_api_request, toInt64OrZero(toString(attributes.output_tokens)), toInt64OrZero(toString(attributes.gen_ai.usage.output_tokens))), is_claude_api_request OR is_generic_usage_row) AS total_output_tokens,
  sumIfState(if(is_claude_api_request, toInt64OrZero(toString(attributes.input_tokens)) + toInt64OrZero(toString(attributes.output_tokens)) + toInt64OrZero(toString(attributes.cache_read_tokens)) + toInt64OrZero(toString(attributes.cache_creation_tokens)), toInt64OrZero(toString(attributes.gen_ai.usage.total_tokens))), is_claude_api_request OR is_generic_usage_row) AS total_tokens,
  sumIfState(if(is_claude_api_request, toInt64OrZero(toString(attributes.cache_read_tokens)), toInt64OrZero(toString(attributes.gen_ai.usage.cache_read.input_tokens))), is_claude_api_request OR is_generic_usage_row) AS cache_read_input_tokens,
  sumIfState(if(is_claude_api_request, toInt64OrZero(toString(attributes.cache_creation_tokens)), toInt64OrZero(toString(attributes.gen_ai.usage.cache_creation.input_tokens))), is_claude_api_request OR is_generic_usage_row) AS cache_creation_input_tokens,
  sumIfState(if(is_claude_api_request, multiIf(toString(attributes.cost_usd) != '', toFloat64OrZero(toString(attributes.cost_usd)), toString(attributes.cost_usd_micros) != '', toFloat64OrZero(toString(attributes.cost_usd_micros)) / 1000000, 0), toFloat64OrZero(toString(attributes.gen_ai.usage.cost))), is_claude_api_request OR is_generic_usage_row) AS total_cost,
  countIfState(is_completed_tool_call) AS total_tool_calls,
  account_type,
  provider,
  if(is_claude_api_request, toString(attributes.query_source), '') AS query_source,
  if(is_claude_api_request, toString(attributes.skill.name), '') AS skill_name,
  if(is_claude_api_request, toString(attributes.agent.name), '') AS agent_name,
  if(is_claude_api_request, toString(attributes.mcp_server.name), '') AS mcp_server_name,
  if(is_claude_api_request, toString(attributes.mcp_tool.name), '') AS mcp_tool_name
FROM telemetry_logs
WHERE (time_unix_nano >= attribute_metrics_cutoff_unix_nano) AND (is_claude_api_request OR is_generic_usage_row OR is_tool_row)
GROUP BY gram_project_id, time_bucket, department_name, job_title, employee_type, division_name, cost_center_name, user_email, model, hook_source, roles, groups, account_type, provider, query_source, skill_name, agent_name, mcp_server_name, mcp_tool_name;
-- drop "trace_summaries_mv" view
DROP VIEW `trace_summaries_mv`;
-- create "trace_summaries_mv" view
CREATE MATERIALIZED VIEW `trace_summaries_mv` TO `trace_summaries` AS SELECT trace_id, gram_project_id, any(gram_deployment_id) AS gram_deployment_id, any(gram_function_id) AS gram_function_id, any(gram_urn) AS gram_urn, any(tool_name) AS tool_name, any(tool_source) AS tool_source, any(event_source) AS event_source, any(user_email) AS user_email, any(hook_source) AS hook_source, any(skill_name) AS skill_name, anyIf(toolset_slug, toolset_slug != '') AS toolset_slug, anyIf(external_user_id, external_user_id != '') AS external_user_id, anyIf(user_id, user_id != '') AS user_id, anyIf(toString(attributes.gram.mcp.match), toString(attributes.gram.mcp.match) != '') AS mcp_match, anyIf(toString(attributes.gram.mcp.server_url), toString(attributes.gram.mcp.server_url) != '') AS mcp_server_url, min(time_unix_nano) AS start_time_unix_nano, toUInt64(count(*)) AS log_count, anyIfState(toInt32OrNull(toString(attributes.http.response.status_code)), toString(attributes.http.response.status_code) != '') AS http_status_code, max(if(toString(attributes.gen_ai.tool.call.result) != '', 1, 0)) AS has_result, max(if(toString(attributes.gram.hook.error) != '', 1, 0)) AS has_error, max(if(toString(attributes.gram.hook.block_reason) != '', 1, 0)) AS has_block, anyIf(toString(attributes.gram.hook.block_reason), toString(attributes.gram.hook.block_reason) != '') AS block_reason, anyIf(account_type, account_type != '') AS account_type, anyIf(provider, provider != '') AS provider FROM telemetry_logs WHERE (trace_id IS NOT NULL) AND (trace_id != '') AND (NOT startsWith(telemetry_logs.gram_urn, 'urn:uuid:')) GROUP BY trace_id, gram_project_id;
