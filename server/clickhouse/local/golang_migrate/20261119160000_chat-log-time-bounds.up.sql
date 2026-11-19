-- create "chat_log_time_bounds" table
CREATE TABLE `gram`.`chat_log_time_bounds` (
  `gram_project_id` UUID COMMENT 'Project the chat belongs to.
@access: confidential',
  `gram_chat_id` String COMMENT 'Chat ID, matching telemetry_logs.gram_chat_id.
@access: confidential',
  `first_time_unix_nano` SimpleAggregateFunction(min, Int64) COMMENT 'Earliest time_unix_nano among the chat logs.
@access: confidential',
  `last_time_unix_nano` SimpleAggregateFunction(max, Int64) COMMENT 'Latest time_unix_nano among the chat logs.
@access: confidential'
) ENGINE = AggregatingMergeTree
PRIMARY KEY (`gram_project_id`, `gram_chat_id`) ORDER BY (`gram_project_id`, `gram_chat_id`) TTL fromUnixTimestamp64Nano(last_time_unix_nano) + toIntervalDay(90) SETTINGS index_granularity = 8192 COMMENT 'Time range of the telemetry logs of each chat, for bounding per-chat log reads. Read with min/max and GROUP BY gram_project_id, gram_chat_id.';
-- create "chat_log_time_bounds_mv" view
CREATE MATERIALIZED VIEW `gram`.`chat_log_time_bounds_mv` TO `gram`.`chat_log_time_bounds` AS SELECT gram_project_id, ifNull(gram_chat_id, '') AS gram_chat_id, min(time_unix_nano) AS first_time_unix_nano, max(time_unix_nano) AS last_time_unix_nano FROM `gram`.`telemetry_logs` WHERE ifNull(gram_chat_id, '') != '' GROUP BY gram_project_id, gram_chat_id;
