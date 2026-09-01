-- Create "mcp_call_summaries" table
CREATE TABLE `mcp_call_summaries` (
  `gram_project_id` UUID,
  `trace_id` FixedString(32),
  `tool_call_id` String,
  `event_source` SimpleAggregateFunction(max, String),
  `tool_name` SimpleAggregateFunction(max, String),
  `toolset_slug` SimpleAggregateFunction(max, String),
  `mcp_server_url` SimpleAggregateFunction(max, String),
  `hook_source` SimpleAggregateFunction(max, String),
  `start_time_unix_nano` SimpleAggregateFunction(min, Int64),
  `http_status_code` SimpleAggregateFunction(max, Int32),
  `has_result` SimpleAggregateFunction(max, UInt8),
  `has_error` SimpleAggregateFunction(max, UInt8),
  INDEX `idx_mcp_call_summaries_start_time` ((start_time_unix_nano)) TYPE minmax GRANULARITY 1
) ENGINE = AggregatingMergeTree
PRIMARY KEY (`gram_project_id`, `trace_id`, `tool_call_id`) ORDER BY (`gram_project_id`, `trace_id`, `tool_call_id`) TTL fromUnixTimestamp64Nano(start_time_unix_nano) + toIntervalDay(90) SETTINGS index_granularity = 8192 COMMENT 'Per-tool-call MCP summaries so drill-down can filter by server without mixing other servers from the same session';
-- Backfill BEFORE creating the MV (same MV-last pattern as tum_breakdown):
-- an MV created first would also ingest rows written while this scan runs.
INSERT INTO `mcp_call_summaries` SELECT gram_project_id, trace_id, multiIf(toString(attributes.tool_use_id) != '', toString(attributes.tool_use_id), toString(attributes.gen_ai.tool.call.id) != '', toString(attributes.gen_ai.tool.call.id), toString(id)) AS tool_call_id, max(event_source) AS event_source, max(tool_name) AS tool_name, max(toolset_slug) AS toolset_slug, max(toString(attributes.gram.mcp.server_url)) AS mcp_server_url, max(hook_source) AS hook_source, min(time_unix_nano) AS start_time_unix_nano, max(toInt32OrZero(toString(attributes.http.response.status_code))) AS http_status_code, max(if(toString(attributes.gen_ai.tool.call.result) != '', 1, 0)) AS has_result, max(if(toString(attributes.gram.hook.error) != '', 1, 0)) AS has_error FROM telemetry_logs WHERE (telemetry_logs.trace_id IS NOT NULL) AND (telemetry_logs.trace_id != '') AND (((telemetry_logs.event_source != 'hook') AND (telemetry_logs.toolset_slug != '')) OR ((telemetry_logs.event_source = 'hook') AND (toString(telemetry_logs.attributes.gram.mcp.server_url) != ''))) GROUP BY gram_project_id, trace_id, tool_call_id;
-- Create "mcp_call_summaries_mv" view — LAST, closing the backfill.
CREATE MATERIALIZED VIEW `mcp_call_summaries_mv` TO `mcp_call_summaries` AS SELECT gram_project_id, trace_id, multiIf(toString(attributes.tool_use_id) != '', toString(attributes.tool_use_id), toString(attributes.gen_ai.tool.call.id) != '', toString(attributes.gen_ai.tool.call.id), toString(id)) AS tool_call_id, max(event_source) AS event_source, max(tool_name) AS tool_name, max(toolset_slug) AS toolset_slug, max(toString(attributes.gram.mcp.server_url)) AS mcp_server_url, max(hook_source) AS hook_source, min(time_unix_nano) AS start_time_unix_nano, max(toInt32OrZero(toString(attributes.http.response.status_code))) AS http_status_code, max(if(toString(attributes.gen_ai.tool.call.result) != '', 1, 0)) AS has_result, max(if(toString(attributes.gram.hook.error) != '', 1, 0)) AS has_error FROM telemetry_logs WHERE (telemetry_logs.trace_id IS NOT NULL) AND (telemetry_logs.trace_id != '') AND (((telemetry_logs.event_source != 'hook') AND (telemetry_logs.toolset_slug != '')) OR ((telemetry_logs.event_source = 'hook') AND (toString(telemetry_logs.attributes.gram.mcp.server_url) != ''))) GROUP BY gram_project_id, trace_id, tool_call_id;
