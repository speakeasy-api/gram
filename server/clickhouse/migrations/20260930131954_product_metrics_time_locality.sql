-- Pause metric writers/readers for the target swap. Preserve all aggregate state.
DROP VIEW gram.product_metric_sums_1m_mv;
DROP VIEW gram.product_metric_sums_1h_mv;
DROP VIEW gram.product_metric_sums_1d_mv;
DROP VIEW gram.product_metric_histograms_1m_mv;
DROP VIEW gram.product_metric_histograms_1h_mv;
DROP VIEW gram.product_metric_histograms_1d_mv;
-- Create "product_metric_histograms_1d_tmp" table
CREATE TABLE `gram`.`product_metric_histograms_1d_tmp` (
  `organization_id` String,
  `project_id` UUID,
  `metric_name` String,
  `bucket` DateTime('UTC'),
  `scope_name` String,
  `scope_version` String,
  `unit` String,
  `number_kind` LowCardinality(String),
  `series_id` String,
  `integer_sum` SimpleAggregateFunction(sum, Int128),
  `floating_sum` SimpleAggregateFunction(sum, Float64),
  `contributions` SimpleAggregateFunction(sum, UInt64),
  `integer_min` SimpleAggregateFunction(min, Int64),
  `integer_max` SimpleAggregateFunction(max, Int64),
  `floating_min` SimpleAggregateFunction(min, Float64),
  `floating_max` SimpleAggregateFunction(max, Float64)
) ENGINE = AggregatingMergeTree
PRIMARY KEY (`organization_id`, `project_id`, `metric_name`, `scope_name`, `scope_version`, `unit`, `bucket`, `series_id`, `number_kind`) ORDER BY (`organization_id`, `project_id`, `metric_name`, `scope_name`, `scope_version`, `unit`, `bucket`, `series_id`, `number_kind`) PARTITION BY (toYYYYMM(bucket)) TTL bucket + toIntervalDay(91) SETTINGS index_granularity = 8192 COMMENT 'Delivery-weighted histogram observations without buckets or quantiles. Mean is total sum divided by total count. No automatic zeros for missing minutes';
-- Copy data from "product_metric_histograms_1d" to "product_metric_histograms_1d_tmp" table
INSERT INTO `gram`.`product_metric_histograms_1d_tmp` SELECT * FROM `gram`.`product_metric_histograms_1d` SETTINGS async_insert=0;
-- Drop "product_metric_histograms_1d" table
DROP TABLE `gram`.`product_metric_histograms_1d`;
-- Rename table "product_metric_histograms_1d_tmp" to "product_metric_histograms_1d"
RENAME TABLE `gram`.`product_metric_histograms_1d_tmp` TO `gram`.`product_metric_histograms_1d`;
-- Create "product_metric_histograms_1h_tmp" table
CREATE TABLE `gram`.`product_metric_histograms_1h_tmp` (
  `organization_id` String,
  `project_id` UUID,
  `metric_name` String,
  `bucket` DateTime('UTC'),
  `scope_name` String,
  `scope_version` String,
  `unit` String,
  `number_kind` LowCardinality(String),
  `series_id` String,
  `integer_sum` SimpleAggregateFunction(sum, Int128),
  `floating_sum` SimpleAggregateFunction(sum, Float64),
  `contributions` SimpleAggregateFunction(sum, UInt64),
  `integer_min` SimpleAggregateFunction(min, Int64),
  `integer_max` SimpleAggregateFunction(max, Int64),
  `floating_min` SimpleAggregateFunction(min, Float64),
  `floating_max` SimpleAggregateFunction(max, Float64)
) ENGINE = AggregatingMergeTree
PRIMARY KEY (`organization_id`, `project_id`, `metric_name`, `scope_name`, `scope_version`, `unit`, `bucket`, `series_id`, `number_kind`) ORDER BY (`organization_id`, `project_id`, `metric_name`, `scope_name`, `scope_version`, `unit`, `bucket`, `series_id`, `number_kind`) PARTITION BY (toYYYYMM(bucket)) TTL bucket + toIntervalDay(91) SETTINGS index_granularity = 8192 COMMENT 'Delivery-weighted histogram observations without buckets or quantiles. Mean is total sum divided by total count. No automatic zeros for missing minutes';
-- Copy data from "product_metric_histograms_1h" to "product_metric_histograms_1h_tmp" table
INSERT INTO `gram`.`product_metric_histograms_1h_tmp` SELECT * FROM `gram`.`product_metric_histograms_1h` SETTINGS async_insert=0;
-- Drop "product_metric_histograms_1h" table
DROP TABLE `gram`.`product_metric_histograms_1h`;
-- Rename table "product_metric_histograms_1h_tmp" to "product_metric_histograms_1h"
RENAME TABLE `gram`.`product_metric_histograms_1h_tmp` TO `gram`.`product_metric_histograms_1h`;
-- Create "product_metric_histograms_1m_tmp" table
CREATE TABLE `gram`.`product_metric_histograms_1m_tmp` (
  `organization_id` String,
  `project_id` UUID,
  `metric_name` String,
  `bucket` DateTime('UTC'),
  `scope_name` String,
  `scope_version` String,
  `unit` String,
  `number_kind` LowCardinality(String),
  `series_id` String,
  `integer_sum` SimpleAggregateFunction(sum, Int128),
  `floating_sum` SimpleAggregateFunction(sum, Float64),
  `contributions` SimpleAggregateFunction(sum, UInt64),
  `integer_min` SimpleAggregateFunction(min, Int64),
  `integer_max` SimpleAggregateFunction(max, Int64),
  `floating_min` SimpleAggregateFunction(min, Float64),
  `floating_max` SimpleAggregateFunction(max, Float64)
) ENGINE = AggregatingMergeTree
PRIMARY KEY (`organization_id`, `project_id`, `metric_name`, `scope_name`, `scope_version`, `unit`, `bucket`, `series_id`, `number_kind`) ORDER BY (`organization_id`, `project_id`, `metric_name`, `scope_name`, `scope_version`, `unit`, `bucket`, `series_id`, `number_kind`) PARTITION BY (toYYYYMM(bucket)) TTL bucket + toIntervalDay(90) SETTINGS index_granularity = 8192 COMMENT 'Delivery-weighted histogram observations without buckets or quantiles. Mean is total sum divided by total count. No automatic zeros for missing minutes';
-- Copy data from "product_metric_histograms_1m" to "product_metric_histograms_1m_tmp" table
INSERT INTO `gram`.`product_metric_histograms_1m_tmp` SELECT * FROM `gram`.`product_metric_histograms_1m` SETTINGS async_insert=0;
-- Drop "product_metric_histograms_1m" table
DROP TABLE `gram`.`product_metric_histograms_1m`;
-- Rename table "product_metric_histograms_1m_tmp" to "product_metric_histograms_1m"
RENAME TABLE `gram`.`product_metric_histograms_1m_tmp` TO `gram`.`product_metric_histograms_1m`;
-- Create "product_metric_sums_1d_tmp" table
CREATE TABLE `gram`.`product_metric_sums_1d_tmp` (
  `organization_id` String,
  `project_id` UUID,
  `metric_name` String,
  `bucket` DateTime('UTC'),
  `scope_name` String,
  `scope_version` String,
  `unit` String,
  `number_kind` LowCardinality(String),
  `series_id` String,
  `integer_sum` SimpleAggregateFunction(sum, Int128),
  `floating_sum` SimpleAggregateFunction(sum, Float64),
  `contributions` SimpleAggregateFunction(sum, UInt64)
) ENGINE = AggregatingMergeTree
PRIMARY KEY (`organization_id`, `project_id`, `metric_name`, `scope_name`, `scope_version`, `unit`, `bucket`, `series_id`, `number_kind`) ORDER BY (`organization_id`, `project_id`, `metric_name`, `scope_name`, `scope_version`, `unit`, `bucket`, `series_id`, `number_kind`) PARTITION BY (toYYYYMM(bucket)) TTL bucket + toIntervalDay(91) SETTINGS index_granularity = 8192 COMMENT 'Monotonic delta sums over half-open UTC minute windows. Explicit sum reads are correct before merges. Late arrivals update retained buckets and current windows are partial';
-- Copy data from "product_metric_sums_1d" to "product_metric_sums_1d_tmp" table
INSERT INTO `gram`.`product_metric_sums_1d_tmp` SELECT * FROM `gram`.`product_metric_sums_1d` SETTINGS async_insert=0;
-- Drop "product_metric_sums_1d" table
DROP TABLE `gram`.`product_metric_sums_1d`;
-- Rename table "product_metric_sums_1d_tmp" to "product_metric_sums_1d"
RENAME TABLE `gram`.`product_metric_sums_1d_tmp` TO `gram`.`product_metric_sums_1d`;
-- Create "product_metric_sums_1h_tmp" table
CREATE TABLE `gram`.`product_metric_sums_1h_tmp` (
  `organization_id` String,
  `project_id` UUID,
  `metric_name` String,
  `bucket` DateTime('UTC'),
  `scope_name` String,
  `scope_version` String,
  `unit` String,
  `number_kind` LowCardinality(String),
  `series_id` String,
  `integer_sum` SimpleAggregateFunction(sum, Int128),
  `floating_sum` SimpleAggregateFunction(sum, Float64),
  `contributions` SimpleAggregateFunction(sum, UInt64)
) ENGINE = AggregatingMergeTree
PRIMARY KEY (`organization_id`, `project_id`, `metric_name`, `scope_name`, `scope_version`, `unit`, `bucket`, `series_id`, `number_kind`) ORDER BY (`organization_id`, `project_id`, `metric_name`, `scope_name`, `scope_version`, `unit`, `bucket`, `series_id`, `number_kind`) PARTITION BY (toYYYYMM(bucket)) TTL bucket + toIntervalDay(91) SETTINGS index_granularity = 8192 COMMENT 'Monotonic delta sums over half-open UTC minute windows. Explicit sum reads are correct before merges. Late arrivals update retained buckets and current windows are partial';
-- Copy data from "product_metric_sums_1h" to "product_metric_sums_1h_tmp" table
INSERT INTO `gram`.`product_metric_sums_1h_tmp` SELECT * FROM `gram`.`product_metric_sums_1h` SETTINGS async_insert=0;
-- Drop "product_metric_sums_1h" table
DROP TABLE `gram`.`product_metric_sums_1h`;
-- Rename table "product_metric_sums_1h_tmp" to "product_metric_sums_1h"
RENAME TABLE `gram`.`product_metric_sums_1h_tmp` TO `gram`.`product_metric_sums_1h`;
-- Create "product_metric_sums_1m_tmp" table
CREATE TABLE `gram`.`product_metric_sums_1m_tmp` (
  `organization_id` String,
  `project_id` UUID,
  `metric_name` String,
  `bucket` DateTime('UTC'),
  `scope_name` String,
  `scope_version` String,
  `unit` String,
  `number_kind` LowCardinality(String),
  `series_id` String,
  `integer_sum` SimpleAggregateFunction(sum, Int128),
  `floating_sum` SimpleAggregateFunction(sum, Float64),
  `contributions` SimpleAggregateFunction(sum, UInt64)
) ENGINE = AggregatingMergeTree
PRIMARY KEY (`organization_id`, `project_id`, `metric_name`, `scope_name`, `scope_version`, `unit`, `bucket`, `series_id`, `number_kind`) ORDER BY (`organization_id`, `project_id`, `metric_name`, `scope_name`, `scope_version`, `unit`, `bucket`, `series_id`, `number_kind`) PARTITION BY (toYYYYMM(bucket)) TTL bucket + toIntervalDay(90) SETTINGS index_granularity = 8192 COMMENT 'Monotonic delta sums over half-open UTC minute windows. Explicit sum reads are correct before merges. Late arrivals update retained buckets and current windows are partial';
-- Copy data from "product_metric_sums_1m" to "product_metric_sums_1m_tmp" table
INSERT INTO `gram`.`product_metric_sums_1m_tmp` SELECT * FROM `gram`.`product_metric_sums_1m` SETTINGS async_insert=0;
-- Drop "product_metric_sums_1m" table
DROP TABLE `gram`.`product_metric_sums_1m`;
-- Rename table "product_metric_sums_1m_tmp" to "product_metric_sums_1m"
RENAME TABLE `gram`.`product_metric_sums_1m_tmp` TO `gram`.`product_metric_sums_1m`;
CREATE MATERIALIZED VIEW gram.product_metric_sums_1m_mv TO gram.product_metric_sums_1m AS SELECT organization_id, project_id, metric_name, toStartOfMinute(event_time) AS bucket, scope_name, scope_version, unit, number_kind, series_id, sum(toInt128(integer_value)) AS integer_sum, sum(floating_value) AS floating_sum, count() AS contributions FROM gram.product_metric_contributions WHERE instrument='counter' GROUP BY organization_id, project_id, metric_name, bucket, scope_name, scope_version, unit, number_kind, series_id;
CREATE MATERIALIZED VIEW gram.product_metric_sums_1h_mv TO gram.product_metric_sums_1h AS SELECT organization_id, project_id, metric_name, toStartOfHour(event_time) AS bucket, scope_name, scope_version, unit, number_kind, series_id, sum(toInt128(integer_value)) AS integer_sum, sum(floating_value) AS floating_sum, count() AS contributions FROM gram.product_metric_contributions WHERE instrument='counter' GROUP BY organization_id, project_id, metric_name, bucket, scope_name, scope_version, unit, number_kind, series_id;
CREATE MATERIALIZED VIEW gram.product_metric_sums_1d_mv TO gram.product_metric_sums_1d AS SELECT organization_id, project_id, metric_name, toStartOfDay(event_time, 'UTC') AS bucket, scope_name, scope_version, unit, number_kind, series_id, sum(toInt128(integer_value)) AS integer_sum, sum(floating_value) AS floating_sum, count() AS contributions FROM gram.product_metric_contributions WHERE instrument='counter' GROUP BY organization_id, project_id, metric_name, bucket, scope_name, scope_version, unit, number_kind, series_id;
CREATE MATERIALIZED VIEW gram.product_metric_histograms_1m_mv TO gram.product_metric_histograms_1m AS SELECT organization_id, project_id, metric_name, toStartOfMinute(event_time) AS bucket, scope_name, scope_version, unit, number_kind, series_id, sum(toInt128(integer_value)) AS integer_sum, sum(floating_value) AS floating_sum, count() AS contributions, min(integer_value) AS integer_min, max(integer_value) AS integer_max, min(floating_value) AS floating_min, max(floating_value) AS floating_max FROM gram.product_metric_contributions WHERE instrument='histogram' GROUP BY organization_id, project_id, metric_name, bucket, scope_name, scope_version, unit, number_kind, series_id;
CREATE MATERIALIZED VIEW gram.product_metric_histograms_1h_mv TO gram.product_metric_histograms_1h AS SELECT organization_id, project_id, metric_name, toStartOfHour(event_time) AS bucket, scope_name, scope_version, unit, number_kind, series_id, sum(toInt128(integer_value)) AS integer_sum, sum(floating_value) AS floating_sum, count() AS contributions, min(integer_value) AS integer_min, max(integer_value) AS integer_max, min(floating_value) AS floating_min, max(floating_value) AS floating_max FROM gram.product_metric_contributions WHERE instrument='histogram' GROUP BY organization_id, project_id, metric_name, bucket, scope_name, scope_version, unit, number_kind, series_id;
CREATE MATERIALIZED VIEW gram.product_metric_histograms_1d_mv TO gram.product_metric_histograms_1d AS SELECT organization_id, project_id, metric_name, toStartOfDay(event_time, 'UTC') AS bucket, scope_name, scope_version, unit, number_kind, series_id, sum(toInt128(integer_value)) AS integer_sum, sum(floating_value) AS floating_sum, count() AS contributions, min(integer_value) AS integer_min, max(integer_value) AS integer_max, min(floating_value) AS floating_min, max(floating_value) AS floating_max FROM gram.product_metric_contributions WHERE instrument='histogram' GROUP BY organization_id, project_id, metric_name, bucket, scope_name, scope_version, unit, number_kind, series_id;
