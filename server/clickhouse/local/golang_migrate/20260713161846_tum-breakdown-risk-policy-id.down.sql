-- reverse: create "tum_breakdown_summaries_mv" view
DROP VIEW `tum_breakdown_summaries_mv`;
-- reverse: create "chat_token_summaries_mv" view
DROP VIEW `chat_token_summaries_mv`;
-- reverse: drop "chat_token_summaries_mv" view
CREATE MATERIALIZED VIEW `chat_token_summaries_mv` TO `chat_token_summaries` AS SELECT gram_project_id, chat_id, toStartOfDay(fromUnixTimestamp64Nano(time_unix_nano, 'UTC')) AS time_bucket, sumIf(toInt64OrZero(toString(attributes.gen_ai.usage.total_tokens)), toString(attributes.gen_ai.usage.total_tokens) != '') AS total_tokens, toUInt64(countIf(startsWith(gram_urn, 'tools:') OR (urn != '') OR (event_source != '') OR (toString(attributes.gen_ai.usage.total_tokens) = ''))) AS stored_event_count FROM telemetry_logs WHERE chat_id != '' GROUP BY gram_project_id, chat_id, time_bucket;
-- reverse: create "tum_breakdown_summaries" table
DROP TABLE `tum_breakdown_summaries`;
-- reverse: create "chat_token_summaries" table
DROP TABLE `chat_token_summaries`;
-- reverse: drop "chat_token_summaries" table
CREATE TABLE `chat_token_summaries` (
  `gram_project_id` UUID,
  `chat_id` String,
  `time_bucket` DateTime('UTC'),
  `hook_source` String,
  `total_tokens` SimpleAggregateFunction(sum, Int64),
  `stored_event_count` SimpleAggregateFunction(sum, UInt64)
) ENGINE = AggregatingMergeTree
PRIMARY KEY (`gram_project_id`, `chat_id`, `time_bucket`) ORDER BY (`gram_project_id`, `chat_id`, `time_bucket`, `hook_source`) TTL time_bucket + toIntervalDay(730) SETTINGS index_granularity = 8192 COMMENT 'Per-chat daily token usage and stored-session evidence, retained beyond the raw telemetry TTL to support tokens-under-management billing across historical billing cycles';
