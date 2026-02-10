-- Create "trace_summaries" table
CREATE TABLE `trace_summaries` (
  `trace_id` FixedString(32),
  `gram_project_id` UUID,
  `gram_deployment_id` SimpleAggregateFunction(any, Nullable(UUID)),
  `gram_function_id` SimpleAggregateFunction(any, Nullable(UUID)),
  `gram_urn` SimpleAggregateFunction(any, String),
  `start_time_unix_nano` SimpleAggregateFunction(min, Int64),
  `log_count` SimpleAggregateFunction(sum, UInt64),
  `http_status_code` AggregateFunction(anyIf, Nullable(Int32), UInt8)
) ENGINE = AggregatingMergeTree
PRIMARY KEY (`gram_project_id`, `trace_id`) ORDER BY (`gram_project_id`, `trace_id`) PARTITION BY (toYYYYMMDD(fromUnixTimestamp64Nano(start_time_unix_nano))) TTL fromUnixTimestamp64Nano(start_time_unix_nano) + toIntervalDay(30) SETTINGS index_granularity = 8192 COMMENT 'Pre-aggregated trace summaries for fast trace-level queries without needing to scan all logs';
-- Create "trace_summaries_mv" view
CREATE MATERIALIZED VIEW `trace_summaries_mv` TO `trace_summaries` AS SELECT trace_id, gram_project_id, any(gram_deployment_id) AS gram_deployment_id, any(gram_function_id) AS gram_function_id, any(gram_urn) AS gram_urn, min(time_unix_nano) AS start_time_unix_nano, toUInt64(count(*)) AS log_count, anyIfState(toInt32OrNull(toString(attributes.http.response.status_code)), toString(attributes.http.response.status_code) != '') AS http_status_code FROM telemetry_logs WHERE (trace_id IS NOT NULL) AND (trace_id != '') GROUP BY trace_id, gram_project_id;
