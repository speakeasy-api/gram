ALTER TABLE `trace_summaries` DROP COLUMN `hook_has_success`;
ALTER TABLE `trace_summaries` DROP COLUMN `hook_has_failure`;
ALTER TABLE `trace_summaries` ADD COLUMN `has_result` SimpleAggregateFunction(max, UInt8);
ALTER TABLE `trace_summaries` ADD COLUMN `has_error` SimpleAggregateFunction(max, UInt8);
-- Drop "trace_summaries_mv" view
DROP VIEW `trace_summaries_mv`;
-- Create "trace_summaries_mv" view
CREATE MATERIALIZED VIEW `trace_summaries_mv` TO `trace_summaries` AS SELECT trace_id, gram_project_id, any(gram_deployment_id) AS gram_deployment_id, any(gram_function_id) AS gram_function_id, any(gram_urn) AS gram_urn, any(tool_name) AS tool_name, any(tool_source) AS tool_source, any(event_source) AS event_source, any(user_email) AS user_email, any(hook_source) AS hook_source, any(skill_name) AS skill_name, min(time_unix_nano) AS start_time_unix_nano, toUInt64(count(*)) AS log_count, anyIfState(toInt32OrNull(toString(attributes.http.response.status_code)), toString(attributes.http.response.status_code) != '') AS http_status_code, max(if(toString(attributes.gen_ai.tool.call.result) != '', 1, 0)) AS has_result, max(if(toString(attributes.gram.hook.error) != '', 1, 0)) AS has_error FROM telemetry_logs WHERE (trace_id IS NOT NULL) AND (trace_id != '') AND (NOT startsWith(telemetry_logs.gram_urn, 'urn:uuid:')) GROUP BY trace_id, gram_project_id;
