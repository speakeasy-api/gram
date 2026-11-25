-- create "mcp_tool_call_summaries" table
CREATE TABLE `gram`.`mcp_tool_call_summaries` (
  `gram_project_id` UUID COMMENT 'Project the tool call belongs to.
@access: confidential',
  `mcp_server_id` String COMMENT 'MCP server ID, empty when the call did not go through one.
@access: confidential',
  `remote_mcp_server_id` String COMMENT 'Remote MCP server ID, empty when none applies.
@access: confidential',
  `toolset_slug` String COMMENT 'Toolset slug of the call.
@access: confidential',
  `meta_mcp_server_id` String COMMENT 'Meta MCP server (Gateway Endpoint) ID when the call went through a gateway, empty otherwise.
@access: confidential',
  `hook_source` LowCardinality(String) COMMENT 'Hook source of the call, empty for non-hook traffic.
@access: confidential',
  `time_bucket` DateTime('UTC') COMMENT 'Start of the 15-minute UTC bucket containing the call.
@access: confidential',
  `gram_urn` String COMMENT 'Tool URN, always starting with tools:.
@access: confidential',
  `total_calls` SimpleAggregateFunction(sum, UInt64) COMMENT 'Number of tool calls.
@access: confidential',
  `success_calls` SimpleAggregateFunction(sum, UInt64) COMMENT 'Tool calls with an HTTP response status from 200 to 299.
@access: confidential',
  `failed_calls` SimpleAggregateFunction(sum, UInt64) COMMENT 'Tool calls with an HTTP response status of 400 or more.
@access: confidential',
  `latency_ms_sum` SimpleAggregateFunction(sum, Float64) COMMENT 'Sum of request durations in milliseconds, zero for calls without a duration. Divide by total_calls for the average.
@access: confidential'
) ENGINE = AggregatingMergeTree
PRIMARY KEY (`gram_project_id`, `mcp_server_id`, `remote_mcp_server_id`, `toolset_slug`, `meta_mcp_server_id`, `hook_source`, `time_bucket`, `gram_urn`) ORDER BY (`gram_project_id`, `mcp_server_id`, `remote_mcp_server_id`, `toolset_slug`, `meta_mcp_server_id`, `hook_source`, `time_bucket`, `gram_urn`) PARTITION BY (toYYYYMM(time_bucket)) TTL time_bucket + toIntervalDay(90) SETTINGS index_granularity = 8192 COMMENT 'Tool call counts and latency per server, hook source, tool and 15-minute bucket';
-- create "mcp_tool_call_summaries_mv" view
CREATE MATERIALIZED VIEW `gram`.`mcp_tool_call_summaries_mv` TO `gram`.`mcp_tool_call_summaries` AS WITH toUnixTimestamp64Nano(toDateTime64('2026-12-02 00:00:00', 9, 'UTC')) AS mcp_tool_call_summaries_cutoff_unix_nano SELECT gram_project_id, mcp_server_id, remote_mcp_server_id, toolset_slug, meta_mcp_server_id, toLowCardinality(hook_source) AS hook_source, toStartOfInterval(fromUnixTimestamp64Nano(time_unix_nano, 'UTC'), toIntervalMinute(15), 'UTC') AS time_bucket, gram_urn, toUInt64(count()) AS total_calls, toUInt64(countIf((toInt32OrZero(toString(attributes.http.response.status_code)) >= 200) AND (toInt32OrZero(toString(attributes.http.response.status_code)) < 300))) AS success_calls, toUInt64(countIf(toInt32OrZero(toString(attributes.http.response.status_code)) >= 400)) AS failed_calls, sum(toFloat64OrZero(toString(attributes.http.server.request.duration)) * 1000) AS latency_ms_sum FROM gram.telemetry_logs WHERE startsWith(gram_urn, 'tools:') AND (time_unix_nano >= mcp_tool_call_summaries_cutoff_unix_nano) GROUP BY gram_project_id, mcp_server_id, remote_mcp_server_id, toolset_slug, meta_mcp_server_id, hook_source, time_bucket, gram_urn;
