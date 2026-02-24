-- reverse: create "trace_summaries_mv" view
DROP VIEW `trace_summaries_mv`;
ALTER TABLE `trace_summaries` DROP COLUMN `event_source`;
ALTER TABLE `trace_summaries` DROP COLUMN `tool_source`;
ALTER TABLE `trace_summaries` DROP COLUMN `tool_name`;
ALTER TABLE `telemetry_logs` DROP INDEX `idx_telemetry_logs_mat_tool_source`;
ALTER TABLE `telemetry_logs` DROP INDEX `idx_telemetry_logs_mat_tool_name`;
ALTER TABLE `telemetry_logs` DROP INDEX `idx_telemetry_logs_mat_event_source`;
ALTER TABLE `telemetry_logs` DROP COLUMN `event_source`;
ALTER TABLE `telemetry_logs` DROP COLUMN `tool_source`;
ALTER TABLE `telemetry_logs` DROP COLUMN `tool_name`;
-- reverse: drop "trace_summaries_mv" view
CREATE MATERIALIZED VIEW `trace_summaries_mv` TO `trace_summaries` AS SELECT trace_id, gram_project_id, any(gram_deployment_id) AS gram_deployment_id, any(gram_function_id) AS gram_function_id, any(gram_urn) AS gram_urn, min(time_unix_nano) AS start_time_unix_nano, toUInt64(count(*)) AS log_count, anyIfState(toInt32OrNull(toString(attributes.http.response.status_code)), toString(attributes.http.response.status_code) != '') AS http_status_code FROM telemetry_logs WHERE (trace_id IS NOT NULL) AND (trace_id != '') GROUP BY trace_id, gram_project_id;
