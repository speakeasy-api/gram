-- Pause all writers through this migration. Preserve delivery-inclusive totals.
CREATE TABLE gram.product_metric_series (
 organization_id String, project_id UUID, metric_name String, scope_name String, scope_version String, unit String,
 instrument LowCardinality(String), number_kind LowCardinality(String), series_id String,
 resource_attributes Array(Tuple(key String, type String, value String)), scope_attributes Array(Tuple(key String, type String, value String)), point_attributes Array(Tuple(key String, type String, value String)),
 first_seen SimpleAggregateFunction(min, DateTime64(9, 'UTC')), last_seen SimpleAggregateFunction(max, DateTime64(9, 'UTC'))
) ENGINE = AggregatingMergeTree
ORDER BY (organization_id, project_id, metric_name, scope_name, scope_version, unit, instrument, series_id, number_kind, resource_attributes, scope_attributes, point_attributes)
TTL toDateTime(last_seen) + INTERVAL 91 DAY
COMMENT 'Typed series identities retained beyond every supported serving window. Duplicate catalogue inserts are idempotent';
INSERT INTO gram.product_metric_series SELECT organization_id, project_id, metric_name, scope_name, scope_version, unit, 'histogram', number_kind,
 hex(SHA256(toJSONString(tuple(organization_id, project_id, metric_name, scope_name, scope_version, unit, 'histogram', number_kind, resource_attributes, scope_attributes, point_attributes)))),
 resource_attributes, scope_attributes, point_attributes, toDateTime64(bucket, 9, 'UTC'), toDateTime64(bucket, 9, 'UTC') FROM gram.product_metric_histograms_1m SETTINGS async_insert=0;
INSERT INTO gram.product_metric_series SELECT organization_id, project_id, metric_name, scope_name, scope_version, unit, 'counter', number_kind,
 hex(SHA256(toJSONString(tuple(organization_id, project_id, metric_name, scope_name, scope_version, unit, 'counter', number_kind, resource_attributes, scope_attributes, point_attributes)))),
 resource_attributes, scope_attributes, point_attributes, toDateTime64(bucket, 9, 'UTC'), toDateTime64(bucket, 9, 'UTC') FROM gram.product_metric_sums_1m SETTINGS async_insert=0;
-- Drop "product_metric_histograms_1m_mv" view
DROP VIEW `gram`.`product_metric_histograms_1m_mv`;
-- Drop "product_metric_sums_1m_mv" view
DROP VIEW `gram`.`product_metric_sums_1m_mv`;
ALTER TABLE `gram`.`product_metric_contributions` ADD COLUMN `series_id` String MATERIALIZED hex(SHA256(toJSONString(tuple(organization_id, project_id, metric_name, scope_name, scope_version, unit, instrument, number_kind, resource_attributes, scope_attributes, point_attributes))));
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
PRIMARY KEY (`organization_id`, `project_id`, `metric_name`, `scope_name`, `scope_version`, `unit`, `series_id`, `bucket`, `number_kind`) ORDER BY (`organization_id`, `project_id`, `metric_name`, `scope_name`, `scope_version`, `unit`, `series_id`, `bucket`, `number_kind`) PARTITION BY (toYYYYMM(bucket)) TTL bucket + toIntervalDay(90) SETTINGS index_granularity = 8192 COMMENT 'Delivery-weighted histogram observations without buckets or quantiles. Mean is total sum divided by total count. No automatic zeros for missing minutes';
-- Copy data from "product_metric_histograms_1m" to "product_metric_histograms_1m_tmp" table
INSERT INTO gram.product_metric_histograms_1m_tmp SELECT organization_id, project_id, metric_name, bucket, scope_name, scope_version, unit, number_kind,
 hex(SHA256(toJSONString(tuple(organization_id, project_id, metric_name, scope_name, scope_version, unit, 'histogram', number_kind, resource_attributes, scope_attributes, point_attributes)))),
 integer_sum, floating_sum, contributions, integer_min, integer_max, floating_min, floating_max FROM gram.product_metric_histograms_1m SETTINGS async_insert=0;
-- Drop "product_metric_histograms_1m" table
DROP TABLE `gram`.`product_metric_histograms_1m`;
-- Rename table "product_metric_histograms_1m_tmp" to "product_metric_histograms_1m"
RENAME TABLE `gram`.`product_metric_histograms_1m_tmp` TO `gram`.`product_metric_histograms_1m`;
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
PRIMARY KEY (`organization_id`, `project_id`, `metric_name`, `scope_name`, `scope_version`, `unit`, `series_id`, `bucket`, `number_kind`) ORDER BY (`organization_id`, `project_id`, `metric_name`, `scope_name`, `scope_version`, `unit`, `series_id`, `bucket`, `number_kind`) PARTITION BY (toYYYYMM(bucket)) TTL bucket + toIntervalDay(90) SETTINGS index_granularity = 8192 COMMENT 'Monotonic delta sums over half-open UTC minute windows. Explicit sum reads are correct before merges. Late arrivals update retained buckets and current windows are partial';
-- Copy data from "product_metric_sums_1m" to "product_metric_sums_1m_tmp" table
INSERT INTO gram.product_metric_sums_1m_tmp SELECT organization_id, project_id, metric_name, bucket, scope_name, scope_version, unit, number_kind,
 hex(SHA256(toJSONString(tuple(organization_id, project_id, metric_name, scope_name, scope_version, unit, 'counter', number_kind, resource_attributes, scope_attributes, point_attributes)))),
 integer_sum, floating_sum, contributions FROM gram.product_metric_sums_1m SETTINGS async_insert=0;
-- Drop "product_metric_sums_1m" table
DROP TABLE `gram`.`product_metric_sums_1m`;
-- Rename table "product_metric_sums_1m_tmp" to "product_metric_sums_1m"
RENAME TABLE `gram`.`product_metric_sums_1m_tmp` TO `gram`.`product_metric_sums_1m`;
-- Create "product_metric_histograms_1d" table
CREATE TABLE `gram`.`product_metric_histograms_1d` (
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
PRIMARY KEY (`organization_id`, `project_id`, `metric_name`, `scope_name`, `scope_version`, `unit`, `series_id`, `bucket`, `number_kind`) ORDER BY (`organization_id`, `project_id`, `metric_name`, `scope_name`, `scope_version`, `unit`, `series_id`, `bucket`, `number_kind`) PARTITION BY (toYYYYMM(bucket)) TTL bucket + toIntervalDay(91) SETTINGS index_granularity = 8192 COMMENT 'Delivery-weighted histogram observations without buckets or quantiles. Mean is total sum divided by total count. No automatic zeros for missing minutes';
-- Create "product_metric_histograms_1h" table
CREATE TABLE `gram`.`product_metric_histograms_1h` (
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
PRIMARY KEY (`organization_id`, `project_id`, `metric_name`, `scope_name`, `scope_version`, `unit`, `series_id`, `bucket`, `number_kind`) ORDER BY (`organization_id`, `project_id`, `metric_name`, `scope_name`, `scope_version`, `unit`, `series_id`, `bucket`, `number_kind`) PARTITION BY (toYYYYMM(bucket)) TTL bucket + toIntervalDay(91) SETTINGS index_granularity = 8192 COMMENT 'Delivery-weighted histogram observations without buckets or quantiles. Mean is total sum divided by total count. No automatic zeros for missing minutes';
-- Create "product_metric_series" table
CREATE TABLE IF NOT EXISTS `gram`.`product_metric_series` (
  `organization_id` String,
  `project_id` UUID,
  `metric_name` String,
  `scope_name` String,
  `scope_version` String,
  `unit` String,
  `instrument` LowCardinality(String),
  `number_kind` LowCardinality(String),
  `series_id` String,
  `resource_attributes` Array(Tuple(`key` String, `type` String, `value` String)),
  `scope_attributes` Array(Tuple(`key` String, `type` String, `value` String)),
  `point_attributes` Array(Tuple(`key` String, `type` String, `value` String)),
  `first_seen` SimpleAggregateFunction(min, DateTime64(9, 'UTC')),
  `last_seen` SimpleAggregateFunction(max, DateTime64(9, 'UTC'))
) ENGINE = AggregatingMergeTree
PRIMARY KEY (`organization_id`, `project_id`, `metric_name`, `scope_name`, `scope_version`, `unit`, `instrument`, `series_id`, `number_kind`, `resource_attributes`, `scope_attributes`, `point_attributes`) ORDER BY (`organization_id`, `project_id`, `metric_name`, `scope_name`, `scope_version`, `unit`, `instrument`, `series_id`, `number_kind`, `resource_attributes`, `scope_attributes`, `point_attributes`) TTL toDateTime(last_seen) + toIntervalDay(91) SETTINGS index_granularity = 8192 COMMENT 'Typed series identities retained beyond every supported serving window. Duplicate catalogue inserts are idempotent';
-- Create "product_metric_sums_1d" table
CREATE TABLE `gram`.`product_metric_sums_1d` (
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
PRIMARY KEY (`organization_id`, `project_id`, `metric_name`, `scope_name`, `scope_version`, `unit`, `series_id`, `bucket`, `number_kind`) ORDER BY (`organization_id`, `project_id`, `metric_name`, `scope_name`, `scope_version`, `unit`, `series_id`, `bucket`, `number_kind`) PARTITION BY (toYYYYMM(bucket)) TTL bucket + toIntervalDay(91) SETTINGS index_granularity = 8192 COMMENT 'Monotonic delta sums over half-open UTC minute windows. Explicit sum reads are correct before merges. Late arrivals update retained buckets and current windows are partial';
-- Create "product_metric_sums_1h" table
CREATE TABLE `gram`.`product_metric_sums_1h` (
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
PRIMARY KEY (`organization_id`, `project_id`, `metric_name`, `scope_name`, `scope_version`, `unit`, `series_id`, `bucket`, `number_kind`) ORDER BY (`organization_id`, `project_id`, `metric_name`, `scope_name`, `scope_version`, `unit`, `series_id`, `bucket`, `number_kind`) PARTITION BY (toYYYYMM(bucket)) TTL bucket + toIntervalDay(91) SETTINGS index_granularity = 8192 COMMENT 'Monotonic delta sums over half-open UTC minute windows. Explicit sum reads are correct before merges. Late arrivals update retained buckets and current windows are partial';
-- Seed coarse tiers from existing delivery-inclusive minute totals, not dedup raw.
INSERT INTO gram.product_metric_sums_1h SELECT * REPLACE (toStartOfHour(bucket) AS bucket) FROM gram.product_metric_sums_1m SETTINGS async_insert=0;
INSERT INTO gram.product_metric_sums_1d SELECT * REPLACE (toStartOfDay(bucket, 'UTC') AS bucket) FROM gram.product_metric_sums_1m SETTINGS async_insert=0;
INSERT INTO gram.product_metric_histograms_1h SELECT * REPLACE (toStartOfHour(bucket) AS bucket) FROM gram.product_metric_histograms_1m SETTINGS async_insert=0;
INSERT INTO gram.product_metric_histograms_1d SELECT * REPLACE (toStartOfDay(bucket, 'UTC') AS bucket) FROM gram.product_metric_histograms_1m SETTINGS async_insert=0;
-- Create "product_metric_histograms_1m_mv" view
CREATE MATERIALIZED VIEW `gram`.`product_metric_histograms_1m_mv` TO `gram`.`product_metric_histograms_1m` AS SELECT organization_id, project_id, metric_name, toStartOfMinute(event_time) AS bucket, scope_name, scope_version, unit, number_kind, series_id, sum(toInt128(integer_value)) AS integer_sum, sum(floating_value) AS floating_sum, count() AS contributions, min(integer_value) AS integer_min, max(integer_value) AS integer_max, min(floating_value) AS floating_min, max(floating_value) AS floating_max FROM gram.product_metric_contributions WHERE instrument = 'histogram' GROUP BY organization_id, project_id, metric_name, bucket, scope_name, scope_version, unit, number_kind, series_id;
-- Create "product_metric_sums_1m_mv" view
CREATE MATERIALIZED VIEW `gram`.`product_metric_sums_1m_mv` TO `gram`.`product_metric_sums_1m` AS SELECT organization_id, project_id, metric_name, toStartOfMinute(event_time) AS bucket, scope_name, scope_version, unit, number_kind, series_id, sum(toInt128(integer_value)) AS integer_sum, sum(floating_value) AS floating_sum, count() AS contributions FROM gram.product_metric_contributions WHERE instrument = 'counter' GROUP BY organization_id, project_id, metric_name, bucket, scope_name, scope_version, unit, number_kind, series_id;
-- Create "product_metric_histograms_1d_mv" view
CREATE MATERIALIZED VIEW `gram`.`product_metric_histograms_1d_mv` TO `gram`.`product_metric_histograms_1d` AS SELECT organization_id, project_id, metric_name, toStartOfDay(event_time, 'UTC') AS bucket, scope_name, scope_version, unit, number_kind, series_id, sum(toInt128(integer_value)) AS integer_sum, sum(floating_value) AS floating_sum, count() AS contributions, min(integer_value) AS integer_min, max(integer_value) AS integer_max, min(floating_value) AS floating_min, max(floating_value) AS floating_max FROM gram.product_metric_contributions WHERE instrument = 'histogram' GROUP BY organization_id, project_id, metric_name, bucket, scope_name, scope_version, unit, number_kind, series_id;
-- Create "product_metric_histograms_1h_mv" view
CREATE MATERIALIZED VIEW `gram`.`product_metric_histograms_1h_mv` TO `gram`.`product_metric_histograms_1h` AS SELECT organization_id, project_id, metric_name, toStartOfHour(event_time) AS bucket, scope_name, scope_version, unit, number_kind, series_id, sum(toInt128(integer_value)) AS integer_sum, sum(floating_value) AS floating_sum, count() AS contributions, min(integer_value) AS integer_min, max(integer_value) AS integer_max, min(floating_value) AS floating_min, max(floating_value) AS floating_max FROM gram.product_metric_contributions WHERE instrument = 'histogram' GROUP BY organization_id, project_id, metric_name, bucket, scope_name, scope_version, unit, number_kind, series_id;
-- Create "product_metric_series_mv" view
CREATE MATERIALIZED VIEW `gram`.`product_metric_series_mv` TO `gram`.`product_metric_series` AS SELECT organization_id, project_id, metric_name, scope_name, scope_version, unit, instrument, number_kind, series_id, resource_attributes, scope_attributes, point_attributes, min(event_time) AS first_seen, max(event_time) AS last_seen FROM gram.product_metric_contributions GROUP BY organization_id, project_id, metric_name, scope_name, scope_version, unit, instrument, number_kind, series_id, resource_attributes, scope_attributes, point_attributes;
-- Create "product_metric_sums_1d_mv" view
CREATE MATERIALIZED VIEW `gram`.`product_metric_sums_1d_mv` TO `gram`.`product_metric_sums_1d` AS SELECT organization_id, project_id, metric_name, toStartOfDay(event_time, 'UTC') AS bucket, scope_name, scope_version, unit, number_kind, series_id, sum(toInt128(integer_value)) AS integer_sum, sum(floating_value) AS floating_sum, count() AS contributions FROM gram.product_metric_contributions WHERE instrument = 'counter' GROUP BY organization_id, project_id, metric_name, bucket, scope_name, scope_version, unit, number_kind, series_id;
-- Create "product_metric_sums_1h_mv" view
CREATE MATERIALIZED VIEW `gram`.`product_metric_sums_1h_mv` TO `gram`.`product_metric_sums_1h` AS SELECT organization_id, project_id, metric_name, toStartOfHour(event_time) AS bucket, scope_name, scope_version, unit, number_kind, series_id, sum(toInt128(integer_value)) AS integer_sum, sum(floating_value) AS floating_sum, count() AS contributions FROM gram.product_metric_contributions WHERE instrument = 'counter' GROUP BY organization_id, project_id, metric_name, bucket, scope_name, scope_version, unit, number_kind, series_id;
