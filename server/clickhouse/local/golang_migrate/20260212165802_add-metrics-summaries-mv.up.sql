-- create "metrics_summaries" table
CREATE TABLE `metrics_summaries` (
  `gram_project_id` UUID,
  `time_bucket` DateTime('UTC'),
  `first_seen_unix_nano` SimpleAggregateFunction(min, Int64),
  `last_seen_unix_nano` SimpleAggregateFunction(max, Int64),
  `total_chats` AggregateFunction(uniqExactIf, String, UInt8),
  `distinct_models` AggregateFunction(uniqExactIf, String, UInt8),
  `distinct_providers` AggregateFunction(uniqExactIf, String, UInt8),
  `total_input_tokens` AggregateFunction(sumIf, Int64, UInt8),
  `total_output_tokens` AggregateFunction(sumIf, Int64, UInt8),
  `total_tokens` AggregateFunction(sumIf, Int64, UInt8),
  `avg_tokens_per_request` AggregateFunction(avgIf, Float64, UInt8),
  `total_chat_requests` AggregateFunction(countIf, UInt8),
  `avg_chat_duration_ms` AggregateFunction(avgIf, Float64, UInt8),
  `finish_reason_stop` AggregateFunction(countIf, UInt8),
  `finish_reason_tool_calls` AggregateFunction(countIf, UInt8),
  `total_tool_calls` AggregateFunction(countIf, UInt8),
  `tool_call_success` AggregateFunction(countIf, UInt8),
  `tool_call_failure` AggregateFunction(countIf, UInt8),
  `avg_tool_duration_ms` AggregateFunction(avgIf, Float64, UInt8),
  `chat_resolution_success` AggregateFunction(countIf, UInt8),
  `chat_resolution_failure` AggregateFunction(countIf, UInt8),
  `chat_resolution_partial` AggregateFunction(countIf, UInt8),
  `chat_resolution_abandoned` AggregateFunction(countIf, UInt8),
  `avg_chat_resolution_score` AggregateFunction(avgIf, Float64, UInt8),
  `models` AggregateFunction(sumMapIf, Map(String, UInt64), UInt8),
  `tool_counts` AggregateFunction(sumMapIf, Map(String, UInt64), UInt8),
  `tool_success_counts` AggregateFunction(sumMapIf, Map(String, UInt64), UInt8),
  `tool_failure_counts` AggregateFunction(sumMapIf, Map(String, UInt64), UInt8)
) ENGINE = AggregatingMergeTree
PRIMARY KEY (`gram_project_id`, `time_bucket`) ORDER BY (`gram_project_id`, `time_bucket`) TTL time_bucket + toIntervalDay(30) SETTINGS index_granularity = 8192 COMMENT 'Pre-aggregated metrics summaries for fast dashboard reads without scanning all telemetry logs';
-- create "metrics_summaries_mv" view
CREATE MATERIALIZED VIEW `metrics_summaries_mv` TO `metrics_summaries` AS SELECT gram_project_id, toStartOfHour(fromUnixTimestamp64Nano(time_unix_nano)) AS time_bucket, min(time_unix_nano) AS first_seen_unix_nano, max(time_unix_nano) AS last_seen_unix_nano, uniqExactIfState(toString(attributes.gen_ai.conversation.id), toString(attributes.gen_ai.conversation.id) != '') AS total_chats, uniqExactIfState(toString(attributes.gen_ai.response.model), toString(attributes.gen_ai.response.model) != '') AS distinct_models, uniqExactIfState(toString(attributes.gen_ai.provider.name), toString(attributes.gen_ai.provider.name) != '') AS distinct_providers, sumIfState(toInt64OrZero(toString(attributes.gen_ai.usage.input_tokens)), toString(attributes.gram.resource.urn) = 'agents:chat:completion') AS total_input_tokens, sumIfState(toInt64OrZero(toString(attributes.gen_ai.usage.output_tokens)), toString(attributes.gram.resource.urn) = 'agents:chat:completion') AS total_output_tokens, sumIfState(toInt64OrZero(toString(attributes.gen_ai.usage.total_tokens)), toString(attributes.gram.resource.urn) = 'agents:chat:completion') AS total_tokens, avgIfState(toFloat64OrZero(toString(attributes.gen_ai.usage.total_tokens)), toString(attributes.gram.resource.urn) = 'agents:chat:completion') AS avg_tokens_per_request, countIfState(toString(attributes.gram.resource.urn) = 'agents:chat:completion') AS total_chat_requests, avgIfState(toFloat64OrZero(toString(attributes.gen_ai.conversation.duration)) * 1000, toString(attributes.gram.resource.urn) = 'agents:chat:completion') AS avg_chat_duration_ms, countIfState(position(toString(attributes.gen_ai.response.finish_reasons), 'stop') > 0) AS finish_reason_stop, countIfState(position(toString(attributes.gen_ai.response.finish_reasons), 'tool_calls') > 0) AS finish_reason_tool_calls, countIfState(startsWith(toString(attributes.gram.tool.urn), 'tools:')) AS total_tool_calls, countIfState(startsWith(toString(attributes.gram.tool.urn), 'tools:') AND (toInt32OrZero(toString(attributes.http.response.status_code)) >= 200) AND (toInt32OrZero(toString(attributes.http.response.status_code)) < 300)) AS tool_call_success, countIfState(startsWith(toString(attributes.gram.tool.urn), 'tools:') AND (toInt32OrZero(toString(attributes.http.response.status_code)) >= 400)) AS tool_call_failure, avgIfState(toFloat64OrZero(toString(attributes.http.server.request.duration)) * 1000, startsWith(toString(attributes.gram.tool.urn), 'tools:')) AS avg_tool_duration_ms, countIfState(evaluation_score_label = 'success') AS chat_resolution_success, countIfState(evaluation_score_label = 'failure') AS chat_resolution_failure, countIfState(evaluation_score_label = 'partial') AS chat_resolution_partial, countIfState(evaluation_score_label = 'abandoned') AS chat_resolution_abandoned, avgIfState(toFloat64OrZero(toString(attributes.gen_ai.evaluation.score.value)), evaluation_score_label != '') AS avg_chat_resolution_score, sumMapIfState(map(toString(attributes.gen_ai.response.model), toUInt64(1)), (toString(attributes.gram.resource.urn) = 'agents:chat:completion') AND (toString(attributes.gen_ai.response.model) != '')) AS models, sumMapIfState(map(gram_urn, toUInt64(1)), startsWith(gram_urn, 'tools:')) AS tool_counts, sumMapIfState(map(gram_urn, toUInt64(1)), startsWith(gram_urn, 'tools:') AND (toInt32OrZero(toString(attributes.http.response.status_code)) >= 200) AND (toInt32OrZero(toString(attributes.http.response.status_code)) < 300)) AS tool_success_counts, sumMapIfState(map(gram_urn, toUInt64(1)), startsWith(gram_urn, 'tools:') AND (toInt32OrZero(toString(attributes.http.response.status_code)) >= 400)) AS tool_failure_counts FROM telemetry_logs GROUP BY gram_project_id, time_bucket;
