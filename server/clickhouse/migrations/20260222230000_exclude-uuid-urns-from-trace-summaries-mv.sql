-- Recreate trace_summaries_mv to exclude chat completion logs (urn:uuid:...) at insert time.
-- This prevents the non-deterministic any(gram_urn) from picking a urn:uuid: value
-- for traces that also contain tool call logs.
DROP VIEW IF EXISTS trace_summaries_mv;
CREATE MATERIALIZED VIEW `trace_summaries_mv` TO `trace_summaries` AS SELECT trace_id, gram_project_id, any(gram_deployment_id) AS gram_deployment_id, any(gram_function_id) AS gram_function_id, any(gram_urn) AS gram_urn, min(time_unix_nano) AS start_time_unix_nano, toUInt64(count(*)) AS log_count, anyIfState(toInt32OrNull(toString(attributes.http.response.status_code)), toString(attributes.http.response.status_code) != '') AS http_status_code FROM telemetry_logs WHERE (trace_id IS NOT NULL) AND (trace_id != '') AND (NOT startsWith(telemetry_logs.gram_urn, 'urn:uuid:')) GROUP BY trace_id, gram_project_id;
