-- Pause contribution writers until rollback completes. Copy retained raw rows
-- without replaying rollup increments. Already replaced copies cannot be restored.
DROP VIEW `gram`.`product_metric_sums_1m_mv`;
DROP VIEW `gram`.`product_metric_histograms_1m_mv`;
-- reverse: rename table "product_metric_contributions_tmp" to "product_metric_contributions"
RENAME TABLE `gram`.`product_metric_contributions` TO `gram`.`product_metric_contributions_tmp`;
-- reverse: drop "product_metric_contributions" table
CREATE TABLE `gram`.`product_metric_contributions` (
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
  `contribution_id` String,
  `event_time` DateTime64(9, 'UTC'),
  `observed_at` DateTime64(9, 'UTC'),
  `ingested_at` DateTime64(9, 'UTC') DEFAULT now64(9),
  `integer_value` Int64,
  `floating_value` Float64
) ENGINE = MergeTree
PRIMARY KEY (`organization_id`, `project_id`, `metric_name`, `event_time`) ORDER BY (`organization_id`, `project_id`, `metric_name`, `event_time`) PARTITION BY (toYYYYMMDD(ingested_at)) TTL toDateTime(ingested_at) + toIntervalDay(7) SETTINGS index_granularity = 8192 COMMENT 'Append-oriented generic measurements retained seven days from ingestion for diagnostics and bounded rebuilds. No exactly-once guarantee';
INSERT INTO `gram`.`product_metric_contributions` SELECT * FROM `gram`.`product_metric_contributions_tmp` SETTINGS async_insert = 0;
-- reverse: create "product_metric_contributions_tmp" table
DROP TABLE `gram`.`product_metric_contributions_tmp`;
CREATE MATERIALIZED VIEW `gram`.`product_metric_histograms_1m_mv` TO `gram`.`product_metric_histograms_1m` AS SELECT organization_id, project_id, metric_name, toStartOfMinute(event_time) AS bucket, scope_name, scope_version, unit, number_kind, resource_attributes, scope_attributes, point_attributes, sum(toInt128(integer_value)) AS integer_sum, sum(floating_value) AS floating_sum, count() AS contributions, min(integer_value) AS integer_min, max(integer_value) AS integer_max, min(floating_value) AS floating_min, max(floating_value) AS floating_max FROM gram.product_metric_contributions WHERE instrument = 'histogram' GROUP BY organization_id, project_id, metric_name, bucket, scope_name, scope_version, unit, number_kind, resource_attributes, scope_attributes, point_attributes;
CREATE MATERIALIZED VIEW `gram`.`product_metric_sums_1m_mv` TO `gram`.`product_metric_sums_1m` AS SELECT organization_id, project_id, metric_name, toStartOfMinute(event_time) AS bucket, scope_name, scope_version, unit, number_kind, resource_attributes, scope_attributes, point_attributes, sum(toInt128(integer_value)) AS integer_sum, sum(floating_value) AS floating_sum, count() AS contributions FROM gram.product_metric_contributions WHERE instrument = 'counter' GROUP BY organization_id, project_id, metric_name, bucket, scope_name, scope_version, unit, number_kind, resource_attributes, scope_attributes, point_attributes;
