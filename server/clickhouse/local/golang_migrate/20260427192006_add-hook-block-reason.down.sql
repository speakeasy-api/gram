-- reverse: create "trace_summaries_mv" view
DROP VIEW `trace_summaries_mv`;
ALTER TABLE `trace_summaries` DROP COLUMN `block_reason`;
ALTER TABLE `trace_summaries` DROP COLUMN `has_block`;
ALTER TABLE `telemetry_logs` DROP INDEX `idx_telemetry_logs_mat_hook_block_reason`;
ALTER TABLE `telemetry_logs` DROP COLUMN `hook_block_reason`;
-- reverse: drop "trace_summaries_mv" view
CREATE MATERIALIZED VIEW `trace_summaries_mv` TO `trace_summaries` AS SELECT trace_id, gram_project_id, any(gram_deployment_id) AS gram_deployment_id, any(gram_function_id) AS gram_function_id, any(gram_urn) AS gram_urn, any(tool_name) AS tool_name, any(tool_source) AS tool_source, any(event_source) AS event_source, any(user_email) AS user_email, any(hook_source) AS hook_source, any(skill_name) AS skill_name, min(time_unix_nano) AS start_time_unix_nano, toUInt64(count(*)) AS log_count, anyIfState(toInt32OrNull(toString(attributes.http.response.status_code)), toString(attributes.http.response.status_code) != '') AS http_status_code, max(if(toString(attributes.gen_ai.tool.call.result) != '', 1, 0)) AS has_result, max(if(toString(attributes.gram.hook.error) != '', 1, 0)) AS has_error FROM telemetry_logs WHERE (trace_id IS NOT NULL) AND (trace_id != '') AND (NOT startsWith(telemetry_logs.gram_urn, 'urn:uuid:')) GROUP BY trace_id, gram_project_id;
