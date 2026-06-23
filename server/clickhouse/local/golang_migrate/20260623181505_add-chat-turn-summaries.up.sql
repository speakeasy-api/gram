-- create "chat_turn_summaries" table
CREATE TABLE `chat_turn_summaries` (
  `gram_project_id` UUID COMMENT 'Gram project that owns the chat session.',
  `chat_id` String COMMENT 'Chat/session identifier from attributes.gen_ai.conversation.id.' CODEC(ZSTD),
  -- turn_id is Gram's provider-agnostic turn key. Claude Code emits this as
  -- attributes.prompt.id; other providers can map their native per-turn ID here.
  `turn_id` String COMMENT 'Provider-agnostic identifier for one user turn within the chat session. For Claude Code this is attributes.prompt.id.' CODEC(ZSTD),
  `query_source` LowCardinality(String) COMMENT 'Claude Code subsystem that issued the request, such as main, subagent, auxiliary, or compact.',
  `skill_name` LowCardinality(String) COMMENT 'Claude Code skill active for the request. Empty when no skill contributed context.',
  `agent_name` LowCardinality(String) COMMENT 'Claude Code agent or subagent type that issued the request. Empty when no agent contributed context.',
  `mcp_server_name` LowCardinality(String) COMMENT 'MCP server attributed by Claude Code to this request. Empty when no MCP server contributed context.',
  `mcp_tool_name` LowCardinality(String) COMMENT 'MCP tool attributed by Claude Code to this request. Empty when no MCP tool contributed context.',
  `model` LowCardinality(String) COMMENT 'Claude model used for the attributed API request.',
  `department_name` LowCardinality(String) COMMENT 'WorkOS department name for the user attributed to the request.',
  `job_title` LowCardinality(String) COMMENT 'WorkOS job title for the user attributed to the request.',
  `employee_type` LowCardinality(String) COMMENT 'WorkOS employee type for the user attributed to the request.',
  `division_name` LowCardinality(String) COMMENT 'WorkOS division name for the user attributed to the request.',
  `cost_center_name` LowCardinality(String) COMMENT 'WorkOS cost center name for the user attributed to the request.',
  `user_email` String COMMENT 'Email of the user attributed to the request.' CODEC(ZSTD),
  `hook_source` LowCardinality(String) COMMENT 'Consuming surface that produced the telemetry row, such as claude-code.',
  `roles` Array(LowCardinality(String)) COMMENT 'WorkOS role slugs for the user attributed to the request.',
  `groups` Array(LowCardinality(String)) COMMENT 'WorkOS group slugs for the user attributed to the request.',
  `start_time_unix_nano` Int64 COMMENT 'Earliest API request timestamp for this chat turn attribution bucket, in Unix nanoseconds.',
  `end_time_unix_nano` Int64 COMMENT 'Latest API request timestamp for this chat turn attribution bucket, in Unix nanoseconds.',
  `request_count` UInt64 COMMENT 'Number of Claude Code api_request rows in this attribution bucket.',
  `input_tokens` Int64 COMMENT 'Input tokens reported by Claude Code api_request rows.',
  `output_tokens` Int64 COMMENT 'Output tokens reported by Claude Code api_request rows.',
  `total_tokens` Int64 COMMENT 'Input, output, cache read, and cache creation tokens summed for this attribution bucket.',
  `cache_read_tokens` Int64 COMMENT 'Prompt-cache read tokens reported by Claude Code api_request rows.',
  `cache_creation_tokens` Int64 COMMENT 'Prompt-cache creation tokens. This is the primary marginal context-added attribution measure.',
  `cost_usd` Float64 COMMENT 'Estimated total API request cost in USD for this attribution bucket.',
  `cost_usd_micros` Int64 COMMENT 'Estimated total API request cost in micro-USD for this attribution bucket.',
  INDEX `idx_chat_turn_summaries_chat_id` (`chat_id`) TYPE bloom_filter(0.01) GRANULARITY 1,
  INDEX `idx_chat_turn_summaries_mcp_server_name` (`mcp_server_name`) TYPE bloom_filter(0.01) GRANULARITY 1
) ENGINE = MergeTree
PRIMARY KEY (`gram_project_id`, `chat_id`, `turn_id`, `query_source`, `skill_name`, `agent_name`, `mcp_server_name`, `mcp_tool_name`, `model`, `department_name`, `job_title`, `employee_type`, `division_name`, `cost_center_name`, `user_email`, `hook_source`, `roles`, `groups`) ORDER BY (`gram_project_id`, `chat_id`, `turn_id`, `query_source`, `skill_name`, `agent_name`, `mcp_server_name`, `mcp_tool_name`, `model`, `department_name`, `job_title`, `employee_type`, `division_name`, `cost_center_name`, `user_email`, `hook_source`, `roles`, `groups`) TTL fromUnixTimestamp64Nano(start_time_unix_nano) + toIntervalDay(30) SETTINGS index_granularity = 8192 COMMENT 'Claude Code per-turn request attribution by chat, turn, MCP server/tool, skill, agent, and user attributes. cache_creation_tokens is the primary context-added measure.';

-- create "chat_turn_summaries_mv" view
CREATE MATERIALIZED VIEW `chat_turn_summaries_mv` TO `chat_turn_summaries` AS WITH toUnixTimestamp64Nano(toDateTime64('2026-06-23 18:15:00', 9, 'UTC')) AS chat_turn_cutoff_unix_nano SELECT gram_project_id, chat_id, toString(attributes.prompt.id) AS turn_id, toString(attributes.query_source) AS query_source, toString(attributes.skill.name) AS skill_name, toString(attributes.agent.name) AS agent_name, toString(attributes.mcp_server.name) AS mcp_server_name, toString(attributes.mcp_tool.name) AS mcp_tool_name, multiIf(toString(attributes.model) != '', toString(attributes.model), toString(attributes.gen_ai.request.model) != '', toString(attributes.gen_ai.request.model), toString(attributes.gen_ai.response.model)) AS model, toString(attributes.user.attributes.department_name) AS department_name, toString(attributes.user.attributes.job_title) AS job_title, toString(attributes.user.attributes.employee_type) AS employee_type, toString(attributes.user.attributes.division_name) AS division_name, toString(attributes.user.attributes.cost_center_name) AS cost_center_name, user_email AS user_email, hook_source, arraySort(JSONExtract(ifNull(toJSONString(attributes.user.roles), '[]'), 'Array(String)')) AS roles, arraySort(JSONExtract(ifNull(toJSONString(attributes.user.groups), '[]'), 'Array(String)')) AS groups, min(time_unix_nano) AS start_time_unix_nano, max(time_unix_nano) AS end_time_unix_nano, toUInt64(count()) AS request_count, sum(toInt64OrZero(toString(attributes.input_tokens))) AS input_tokens, sum(toInt64OrZero(toString(attributes.output_tokens))) AS output_tokens, sum(toInt64OrZero(toString(attributes.input_tokens))) + sum(toInt64OrZero(toString(attributes.output_tokens))) + sum(toInt64OrZero(toString(attributes.cache_read_tokens))) + sum(toInt64OrZero(toString(attributes.cache_creation_tokens))) AS total_tokens, sum(toInt64OrZero(toString(attributes.cache_read_tokens))) AS cache_read_tokens, sum(toInt64OrZero(toString(attributes.cache_creation_tokens))) AS cache_creation_tokens, sum(toFloat64OrZero(toString(attributes.cost_usd))) AS cost_usd, sum(toInt64OrZero(toString(attributes.cost_usd_micros))) AS cost_usd_micros FROM telemetry_logs WHERE (time_unix_nano >= chat_turn_cutoff_unix_nano) AND (chat_id != '') AND (toString(attributes.prompt.id) != '') AND ((toString(attributes.event.name) = 'api_request') OR (body = 'claude_code.api_request')) AND ((service_name = 'claude-code') OR (toString(resource_attributes.service.name) = 'claude-code') OR startsWith(body, 'claude_code.')) GROUP BY gram_project_id, chat_id, turn_id, query_source, skill_name, agent_name, mcp_server_name, mcp_tool_name, model, department_name, job_title, employee_type, division_name, cost_center_name, user_email, hook_source, roles, groups;
