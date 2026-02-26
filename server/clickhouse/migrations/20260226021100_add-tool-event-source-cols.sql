-- Drop "trace_summaries_mv" view
DROP VIEW `trace_summaries_mv`;
ALTER TABLE `telemetry_logs` ADD COLUMN `tool_name` String MATERIALIZED toString(attributes.gram.tool.name) COMMENT 'Tool name (materialized from attributes.gram.tool.name).';
ALTER TABLE `telemetry_logs` ADD COLUMN `tool_source` String MATERIALIZED toString(attributes.gram.tool_call.source) COMMENT 'Tool call source (materialized from attributes.gram.tool_call.source).';
ALTER TABLE `telemetry_logs` ADD COLUMN `event_source` String MATERIALIZED toString(attributes.gram.event.source) COMMENT 'Event source (materialized from attributes.gram.event.source).';
ALTER TABLE `telemetry_logs` ADD INDEX `idx_telemetry_logs_mat_event_source` ((event_source)) TYPE bloom_filter(0.01) GRANULARITY 1;
ALTER TABLE `telemetry_logs` ADD INDEX `idx_telemetry_logs_mat_tool_name` ((tool_name)) TYPE bloom_filter(0.01) GRANULARITY 1;
ALTER TABLE `telemetry_logs` ADD INDEX `idx_telemetry_logs_mat_tool_source` ((tool_source)) TYPE bloom_filter(0.01) GRANULARITY 1;
ALTER TABLE `trace_summaries` ADD COLUMN `tool_name` SimpleAggregateFunction(any, String);
ALTER TABLE `trace_summaries` ADD COLUMN `tool_source` SimpleAggregateFunction(any, String);
ALTER TABLE `trace_summaries` ADD COLUMN `event_source` SimpleAggregateFunction(any, String);
-- Create "trace_summaries_mv" view
CREATE MATERIALIZED VIEW `trace_summaries_mv` TO `trace_summaries` AS SELECT trace_id, gram_project_id, any(gram_deployment_id) AS gram_deployment_id, any(gram_function_id) AS gram_function_id, any(gram_urn) AS gram_urn, any(tool_name) AS tool_name, any(tool_source) AS tool_source, any(event_source) AS event_source, min(time_unix_nano) AS start_time_unix_nano, toUInt64(count(*)) AS log_count, anyIfState(toInt32OrNull(toString(attributes.http.response.status_code)), toString(attributes.http.response.status_code) != '') AS http_status_code FROM telemetry_logs WHERE (trace_id IS NOT NULL) AND (trace_id != '') GROUP BY trace_id, gram_project_id;
