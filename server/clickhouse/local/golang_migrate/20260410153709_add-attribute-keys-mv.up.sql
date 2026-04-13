-- create "attribute_keys" table
CREATE TABLE `attribute_keys` (
  `gram_project_id` UUID,
  `attribute_key` String,
  `first_seen_unix_nano` SimpleAggregateFunction(min, Int64),
  `last_seen_unix_nano` SimpleAggregateFunction(max, Int64)
) ENGINE = AggregatingMergeTree
PRIMARY KEY (`gram_project_id`, `attribute_key`) ORDER BY (`gram_project_id`, `attribute_key`) TTL fromUnixTimestamp64Nano(last_seen_unix_nano) + toIntervalDay(30) SETTINGS index_granularity = 8192 COMMENT 'Pre-aggregated attribute keys per project for fast key listing without scanning telemetry_logs JSON';
-- create "attribute_keys_mv" view
CREATE MATERIALIZED VIEW `attribute_keys_mv` TO `attribute_keys` AS SELECT gram_project_id, arrayJoin(JSONAllPaths(attributes)) AS attribute_key, min(time_unix_nano) AS first_seen_unix_nano, max(time_unix_nano) AS last_seen_unix_nano FROM telemetry_logs GROUP BY gram_project_id, attribute_key;
