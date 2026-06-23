-- Create "chat_turn_summaries" table
CREATE TABLE `chat_turn_summaries` (
  `gram_project_id` UUID,
  `chat_id` String,
  `prompt_id` String,
  `query_source` String,
  `skill_name` String,
  `agent_name` String,
  `mcp_server_name` String,
  `mcp_tool_name` String,
  `model` String,
  `department_name` String,
  `job_title` String,
  `employee_type` String,
  `division_name` String,
  `cost_center_name` String,
  `user_email` String,
  `hook_source` String,
  `roles` Array(String),
  `groups` Array(String),
  `start_time_unix_nano` SimpleAggregateFunction(min, Int64),
  `end_time_unix_nano` SimpleAggregateFunction(max, Int64),
  `request_count` SimpleAggregateFunction(sum, UInt64),
  `input_tokens` SimpleAggregateFunction(sum, Int64),
  `output_tokens` SimpleAggregateFunction(sum, Int64),
  `total_tokens` SimpleAggregateFunction(sum, Int64),
  `cache_read_tokens` SimpleAggregateFunction(sum, Int64),
  `cache_creation_tokens` SimpleAggregateFunction(sum, Int64),
  `cost_usd` SimpleAggregateFunction(sum, Float64),
  `cost_usd_micros` SimpleAggregateFunction(sum, Int64),
  INDEX `idx_chat_turn_summaries_chat_id` (`chat_id`) TYPE bloom_filter(0.01) GRANULARITY 1,
  INDEX `idx_chat_turn_summaries_mcp_server_name` (`mcp_server_name`) TYPE bloom_filter(0.01) GRANULARITY 1
) ENGINE = AggregatingMergeTree
PRIMARY KEY (`gram_project_id`, `chat_id`, `prompt_id`, `query_source`, `skill_name`, `agent_name`, `mcp_server_name`, `mcp_tool_name`, `model`, `department_name`, `job_title`, `employee_type`, `division_name`, `cost_center_name`, `user_email`, `hook_source`, `roles`, `groups`) ORDER BY (`gram_project_id`, `chat_id`, `prompt_id`, `query_source`, `skill_name`, `agent_name`, `mcp_server_name`, `mcp_tool_name`, `model`, `department_name`, `job_title`, `employee_type`, `division_name`, `cost_center_name`, `user_email`, `hook_source`, `roles`, `groups`) TTL fromUnixTimestamp64Nano(start_time_unix_nano) + toIntervalDay(30) SETTINGS index_granularity = 8192 COMMENT 'Claude Code per-turn request attribution by chat, prompt, MCP server/tool, skill, agent, and user attributes. cache_creation_tokens is the primary context-added measure.';

-- Create "chat_turn_summaries_mv" view
CREATE MATERIALIZED VIEW `chat_turn_summaries_mv` TO `chat_turn_summaries` AS WITH toUnixTimestamp64Nano(toDateTime64('2026-06-23 18:15:00', 9, 'UTC')) AS chat_turn_cutoff_unix_nano SELECT gram_project_id, chat_id, toString(attributes.prompt.id) AS prompt_id, toString(attributes.query_source) AS query_source, toString(attributes.skill.name) AS skill_name, toString(attributes.agent.name) AS agent_name, toString(attributes.mcp_server.name) AS mcp_server_name, toString(attributes.mcp_tool.name) AS mcp_tool_name, multiIf(toString(attributes.model) != '', toString(attributes.model), toString(attributes.gen_ai.request.model) != '', toString(attributes.gen_ai.request.model), toString(attributes.gen_ai.response.model)) AS model, toString(attributes.user.attributes.department_name) AS department_name, toString(attributes.user.attributes.job_title) AS job_title, toString(attributes.user.attributes.employee_type) AS employee_type, toString(attributes.user.attributes.division_name) AS division_name, toString(attributes.user.attributes.cost_center_name) AS cost_center_name, user_email AS user_email, hook_source, arraySort(JSONExtract(ifNull(toJSONString(attributes.user.roles), '[]'), 'Array(String)')) AS roles, arraySort(JSONExtract(ifNull(toJSONString(attributes.user.groups), '[]'), 'Array(String)')) AS groups, min(time_unix_nano) AS start_time_unix_nano, max(time_unix_nano) AS end_time_unix_nano, toUInt64(count()) AS request_count, sum(toInt64OrZero(toString(attributes.input_tokens))) AS input_tokens, sum(toInt64OrZero(toString(attributes.output_tokens))) AS output_tokens, sum(toInt64OrZero(toString(attributes.input_tokens))) + sum(toInt64OrZero(toString(attributes.output_tokens))) + sum(toInt64OrZero(toString(attributes.cache_read_tokens))) + sum(toInt64OrZero(toString(attributes.cache_creation_tokens))) AS total_tokens, sum(toInt64OrZero(toString(attributes.cache_read_tokens))) AS cache_read_tokens, sum(toInt64OrZero(toString(attributes.cache_creation_tokens))) AS cache_creation_tokens, sum(toFloat64OrZero(toString(attributes.cost_usd))) AS cost_usd, sum(toInt64OrZero(toString(attributes.cost_usd_micros))) AS cost_usd_micros FROM telemetry_logs WHERE (time_unix_nano >= chat_turn_cutoff_unix_nano) AND (chat_id != '') AND (toString(attributes.prompt.id) != '') AND ((toString(attributes.event.name) = 'api_request') OR (body = 'claude_code.api_request')) AND ((service_name = 'claude-code') OR (toString(resource_attributes.service.name) = 'claude-code') OR startsWith(body, 'claude_code.')) GROUP BY gram_project_id, chat_id, prompt_id, query_source, skill_name, agent_name, mcp_server_name, mcp_tool_name, model, department_name, job_title, employee_type, division_name, cost_center_name, user_email, hook_source, roles, groups;

-- Backfill retained historical rows before the live MV cutoff.
INSERT INTO `chat_turn_summaries` WITH toUnixTimestamp64Nano(toDateTime64('2026-06-23 18:15:00', 9, 'UTC')) AS chat_turn_cutoff_unix_nano SELECT gram_project_id, chat_id, toString(attributes.prompt.id) AS prompt_id, toString(attributes.query_source) AS query_source, toString(attributes.skill.name) AS skill_name, toString(attributes.agent.name) AS agent_name, toString(attributes.mcp_server.name) AS mcp_server_name, toString(attributes.mcp_tool.name) AS mcp_tool_name, multiIf(toString(attributes.model) != '', toString(attributes.model), toString(attributes.gen_ai.request.model) != '', toString(attributes.gen_ai.request.model), toString(attributes.gen_ai.response.model)) AS model, toString(attributes.user.attributes.department_name) AS department_name, toString(attributes.user.attributes.job_title) AS job_title, toString(attributes.user.attributes.employee_type) AS employee_type, toString(attributes.user.attributes.division_name) AS division_name, toString(attributes.user.attributes.cost_center_name) AS cost_center_name, user_email AS user_email, hook_source, arraySort(JSONExtract(ifNull(toJSONString(attributes.user.roles), '[]'), 'Array(String)')) AS roles, arraySort(JSONExtract(ifNull(toJSONString(attributes.user.groups), '[]'), 'Array(String)')) AS groups, min(time_unix_nano) AS start_time_unix_nano, max(time_unix_nano) AS end_time_unix_nano, toUInt64(count()) AS request_count, sum(toInt64OrZero(toString(attributes.input_tokens))) AS input_tokens, sum(toInt64OrZero(toString(attributes.output_tokens))) AS output_tokens, sum(toInt64OrZero(toString(attributes.input_tokens))) + sum(toInt64OrZero(toString(attributes.output_tokens))) + sum(toInt64OrZero(toString(attributes.cache_read_tokens))) + sum(toInt64OrZero(toString(attributes.cache_creation_tokens))) AS total_tokens, sum(toInt64OrZero(toString(attributes.cache_read_tokens))) AS cache_read_tokens, sum(toInt64OrZero(toString(attributes.cache_creation_tokens))) AS cache_creation_tokens, sum(toFloat64OrZero(toString(attributes.cost_usd))) AS cost_usd, sum(toInt64OrZero(toString(attributes.cost_usd_micros))) AS cost_usd_micros FROM telemetry_logs WHERE (time_unix_nano >= chat_turn_cutoff_unix_nano - (30 * 24 * 60 * 60 * 1000000000)) AND (time_unix_nano < chat_turn_cutoff_unix_nano) AND (chat_id != '') AND (toString(attributes.prompt.id) != '') AND ((toString(attributes.event.name) = 'api_request') OR (body = 'claude_code.api_request')) AND ((service_name = 'claude-code') OR (toString(resource_attributes.service.name) = 'claude-code') OR startsWith(body, 'claude_code.')) GROUP BY gram_project_id, chat_id, prompt_id, query_source, skill_name, agent_name, mcp_server_name, mcp_tool_name, model, department_name, job_title, employee_type, division_name, cost_center_name, user_email, hook_source, roles, groups;
