-- drop "chat_token_summaries" table
DROP TABLE `chat_token_summaries`;
-- create "chat_token_summaries" table
CREATE TABLE `chat_token_summaries` (
  `gram_project_id` UUID,
  `chat_id` String,
  `time_bucket` DateTime('UTC'),
  `hook_source` String,
  `total_tokens` SimpleAggregateFunction(sum, Int64),
  `stored_event_count` SimpleAggregateFunction(sum, UInt64)
) ENGINE = AggregatingMergeTree
PRIMARY KEY (`gram_project_id`, `chat_id`, `time_bucket`) ORDER BY (`gram_project_id`, `chat_id`, `time_bucket`, `hook_source`) TTL time_bucket + toIntervalDay(730) SETTINGS index_granularity = 8192 COMMENT 'Per-chat daily token usage and stored-session evidence, retained beyond the raw telemetry TTL to support tokens-under-management billing across historical billing cycles';
-- create "tum_breakdown_summaries" table
CREATE TABLE `tum_breakdown_summaries` (
  `gram_project_id` UUID,
  `chat_id` String,
  `time_bucket` DateTime('UTC'),
  `hook_source` String,
  `model` String,
  `user_email` String,
  `division_name` String,
  `roles` Array(String),
  `input_tokens` SimpleAggregateFunction(sum, Int64),
  `output_tokens` SimpleAggregateFunction(sum, Int64),
  `total_tokens` SimpleAggregateFunction(sum, Int64)
) ENGINE = AggregatingMergeTree
PRIMARY KEY (`gram_project_id`, `time_bucket`, `chat_id`, `hook_source`, `model`, `user_email`, `division_name`, `roles`) ORDER BY (`gram_project_id`, `time_bucket`, `chat_id`, `hook_source`, `model`, `user_email`, `division_name`, `roles`) TTL time_bucket + toIntervalDay(730) SETTINGS index_granularity = 8192 COMMENT 'Per-chat daily billed token usage broken down by consuming surface and user identity, retained beyond the raw telemetry TTL to power the billing page breakdowns across historical billing cycles';
-- drop "chat_token_summaries_mv" view
DROP VIEW `chat_token_summaries_mv`;
-- create "chat_token_summaries_mv" view
CREATE MATERIALIZED VIEW `chat_token_summaries_mv` TO `chat_token_summaries` AS SELECT gram_project_id, chat_id, toStartOfDay(fromUnixTimestamp64Nano(time_unix_nano, 'UTC')) AS time_bucket, hook_source, sumIf(toInt64OrZero(toString(attributes.gen_ai.usage.total_tokens)), toString(attributes.gen_ai.usage.total_tokens) != '') AS total_tokens, toUInt64(countIf(startsWith(gram_urn, 'tools:') OR (urn != '') OR (event_source != '') OR (toString(attributes.gen_ai.usage.total_tokens) = ''))) AS stored_event_count FROM telemetry_logs WHERE chat_id != '' GROUP BY gram_project_id, chat_id, time_bucket, hook_source;
-- create "tum_breakdown_summaries_mv" view
CREATE MATERIALIZED VIEW `tum_breakdown_summaries_mv` TO `tum_breakdown_summaries` AS SELECT gram_project_id, chat_id, toStartOfDay(fromUnixTimestamp64Nano(time_unix_nano, 'UTC')) AS time_bucket, hook_source, toString(attributes.gen_ai.response.model) AS model, user_email, toString(attributes.user.attributes.division_name) AS division_name, arraySort(JSONExtract(ifNull(toJSONString(attributes.user.roles), '[]'), 'Array(String)')) AS roles, sum(toInt64OrZero(toString(attributes.gen_ai.usage.input_tokens))) AS input_tokens, sum(toInt64OrZero(toString(attributes.gen_ai.usage.output_tokens))) AS output_tokens, sum(toInt64OrZero(toString(attributes.gen_ai.usage.total_tokens))) AS total_tokens FROM telemetry_logs WHERE (chat_id != '') AND (toString(attributes.gen_ai.usage.total_tokens) != '') GROUP BY gram_project_id, chat_id, time_bucket, hook_source, model, user_email, division_name, roles;
