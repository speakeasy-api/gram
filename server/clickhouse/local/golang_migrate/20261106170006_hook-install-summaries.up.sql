-- create "hook_install_summaries" table
CREATE TABLE `gram`.`hook_install_summaries` (
  `organization_id` String COMMENT 'Organization the install reports to. Empty when the row could not be tied to an org.
@access: confidential',
  `gram_project_id` UUID COMMENT 'Project the install reports to.
@access: confidential',
  `user_email` String COMMENT 'Email of the user the hook events were attributed to, empty when none.
@access: confidential-pii',
  `hostname` String COMMENT 'Hostname the hook reported, empty when none.
@access: confidential-pii',
  `harness` LowCardinality(String) COMMENT 'Coding agent the hook ran in, from the device header or else the hook source.
@access: confidential',
  `day` Date COMMENT 'UTC day of the events.
@access: confidential',
  `binary_version` AggregateFunction(argMaxIf, String, Int64, UInt8) COMMENT 'Latest reported speakeasy-hooks binary version.
@access: confidential',
  `endpoint_generation` AggregateFunction(argMaxIf, String, Int64, UInt8) COMMENT 'Latest endpoint generation: legacy_unkeyed, legacy_keyed, ingest_script or ingest_binary.
@access: confidential',
  `install_route` AggregateFunction(argMaxIf, String, Int64, UInt8) COMMENT 'Latest reported install route, such as marketplace or download.
@access: confidential',
  `first_seen` SimpleAggregateFunction(min, DateTime64(9, 'UTC')) COMMENT 'Time of the earliest event.
@access: confidential',
  `last_seen` SimpleAggregateFunction(max, DateTime64(9, 'UTC')) COMMENT 'Time of the latest event.
@access: confidential',
  `event_count` SimpleAggregateFunction(sum, UInt64) COMMENT 'Hook rows recorded.
@access: confidential',
  `error_count` SimpleAggregateFunction(sum, UInt64) COMMENT 'Hook rows that carried an error, plus delivery failure reports.
@access: confidential'
) ENGINE = AggregatingMergeTree
PRIMARY KEY (`organization_id`, `gram_project_id`, `user_email`, `hostname`, `harness`, `day`) ORDER BY (`organization_id`, `gram_project_id`, `user_email`, `hostname`, `harness`, `day`) PARTITION BY (toMonday(day)) TTL day + toIntervalDay(90) SETTINGS index_granularity = 8192 COMMENT 'Daily per-install summary of hooks versions, endpoint generations, install routes and activity';
-- create "hook_install_summaries_mv" view
CREATE MATERIALIZED VIEW `gram`.`hook_install_summaries_mv` TO `gram`.`hook_install_summaries` AS SELECT toString(attributes.gram.org.id) AS organization_id, gram_project_id, user_email, toString(attributes.gram.hook.hostname) AS hostname, toLowCardinality(if(toString(attributes.gram.hook.device.harness) != '', toString(attributes.gram.hook.device.harness), hook_source)) AS harness, toDate(fromUnixTimestamp64Nano(time_unix_nano, 'UTC')) AS day, argMaxIfState(toString(attributes.gram.hook.device.binary_version), time_unix_nano, toString(attributes.gram.hook.device.binary_version) != '') AS binary_version, argMaxIfState(toString(attributes.gram.hook.endpoint_generation), time_unix_nano, toString(attributes.gram.hook.endpoint_generation) != '') AS endpoint_generation, argMaxIfState(toString(attributes.gram.hook.device.install_route), time_unix_nano, toString(attributes.gram.hook.device.install_route) != '') AS install_route, min(fromUnixTimestamp64Nano(time_unix_nano, 'UTC')) AS first_seen, max(fromUnixTimestamp64Nano(time_unix_nano, 'UTC')) AS last_seen, toUInt64(count()) AS event_count, toUInt64(countIf((event_source = 'hook_diagnostic') OR (toString(attributes.gram.hook.error) != ''))) AS error_count FROM gram.telemetry_logs WHERE event_source IN ('hook', 'hook_diagnostic') GROUP BY organization_id, gram_project_id, user_email, hostname, harness, day;
