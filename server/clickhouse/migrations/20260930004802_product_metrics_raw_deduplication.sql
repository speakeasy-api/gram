-- Pause contribution writers until this migration completes. The default-off
-- subscriber must remain disabled during initial rollout. Copying retained raw
-- data must not replay increments into the live rollups.
DROP VIEW `gram`.`product_metric_sums_1m_mv`;
DROP VIEW `gram`.`product_metric_histograms_1m_mv`;
-- Create "product_metric_contributions_tmp" table
CREATE TABLE `gram`.`product_metric_contributions_tmp` (
  `organization_id` String,
  `project_id` UUID,
  `metric_name` String,
  `scope_name` String,
  `scope_version` String,
  `unit` String,
  `instrument` LowCardinality(String),
  `description` String,
  `number_kind` LowCardinality(String),
  `resource_attributes` Array(Tuple(`key` String, `type` String, `value` String)),
  `scope_attributes` Array(Tuple(`key` String, `type` String, `value` String)),
  `point_attributes` Array(Tuple(`key` String, `type` String, `value` String)),
  `contribution_id` String COMMENT 'Producer-selected immutable observation ID, unique within organization, project and instrumentation scope/version',
  `event_time` DateTime64(9, 'UTC'),
  `observed_at` DateTime64(9, 'UTC'),
  `ingested_at` DateTime64(9, 'UTC') DEFAULT now64(9) COMMENT 'Selects the latest retained delivery copy, not a producer correction version',
  `integer_value` Int64,
  `floating_value` Float64
) ENGINE = ReplacingMergeTree(ingested_at)
PRIMARY KEY (`organization_id`, `project_id`, `scope_name`, `scope_version`, `contribution_id`) ORDER BY (`organization_id`, `project_id`, `scope_name`, `scope_version`, `contribution_id`) PARTITION BY (toYYYYMM(event_time)) TTL toStartOfMinute(event_time) + toIntervalDay(90) SETTINGS index_granularity = 8192 COMMENT 'Immutable producer-ID contributions retained 90 days by event minute for debugging and manual deduplicated rollup rebuilds. Raw replacement does not deduplicate live rollups';
-- Copy data from "product_metric_contributions" to "product_metric_contributions_tmp" table
INSERT INTO `gram`.`product_metric_contributions_tmp` SELECT * FROM `gram`.`product_metric_contributions` SETTINGS async_insert = 0;
-- Drop "product_metric_contributions" table
DROP TABLE `gram`.`product_metric_contributions`;
-- Rename table "product_metric_contributions_tmp" to "product_metric_contributions"
RENAME TABLE `gram`.`product_metric_contributions_tmp` TO `gram`.`product_metric_contributions`;
-- Reattach incremental views to the replacement source. Existing rollup state
-- stays intact and duplicate-inclusive. Raw replacement does not repair it.
CREATE MATERIALIZED VIEW `gram`.`product_metric_histograms_1m_mv` TO `gram`.`product_metric_histograms_1m` AS SELECT organization_id, project_id, metric_name, toStartOfMinute(event_time) AS bucket, scope_name, scope_version, unit, number_kind, resource_attributes, scope_attributes, point_attributes, sum(toInt128(integer_value)) AS integer_sum, sum(floating_value) AS floating_sum, count() AS contributions, min(integer_value) AS integer_min, max(integer_value) AS integer_max, min(floating_value) AS floating_min, max(floating_value) AS floating_max FROM gram.product_metric_contributions WHERE instrument = 'histogram' GROUP BY organization_id, project_id, metric_name, bucket, scope_name, scope_version, unit, number_kind, resource_attributes, scope_attributes, point_attributes;
CREATE MATERIALIZED VIEW `gram`.`product_metric_sums_1m_mv` TO `gram`.`product_metric_sums_1m` AS SELECT organization_id, project_id, metric_name, toStartOfMinute(event_time) AS bucket, scope_name, scope_version, unit, number_kind, resource_attributes, scope_attributes, point_attributes, sum(toInt128(integer_value)) AS integer_sum, sum(floating_value) AS floating_sum, count() AS contributions FROM gram.product_metric_contributions WHERE instrument = 'counter' GROUP BY organization_id, project_id, metric_name, bucket, scope_name, scope_version, unit, number_kind, resource_attributes, scope_attributes, point_attributes;
