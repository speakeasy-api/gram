-- Create "product_metric_contributions" table
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
-- Create "product_metric_histograms_1m" table
CREATE TABLE `gram`.`product_metric_histograms_1m` (
  `organization_id` String,
  `project_id` UUID,
  `metric_name` String,
  `bucket` DateTime('UTC'),
  `scope_name` String,
  `scope_version` String,
  `unit` String,
  `number_kind` LowCardinality(String),
  `resource_attributes` Array(Tuple(`key` String, `type` String, `value` String)),
  `scope_attributes` Array(Tuple(`key` String, `type` String, `value` String)),
  `point_attributes` Array(Tuple(`key` String, `type` String, `value` String)),
  `integer_sum` SimpleAggregateFunction(sum, Int128),
  `floating_sum` SimpleAggregateFunction(sum, Float64),
  `contributions` SimpleAggregateFunction(sum, UInt64),
  `integer_min` SimpleAggregateFunction(min, Int64),
  `integer_max` SimpleAggregateFunction(max, Int64),
  `floating_min` SimpleAggregateFunction(min, Float64),
  `floating_max` SimpleAggregateFunction(max, Float64)
) ENGINE = AggregatingMergeTree
PRIMARY KEY (`organization_id`, `project_id`, `metric_name`, `bucket`, `scope_name`, `scope_version`, `unit`, `number_kind`, `resource_attributes`, `scope_attributes`, `point_attributes`) ORDER BY (`organization_id`, `project_id`, `metric_name`, `bucket`, `scope_name`, `scope_version`, `unit`, `number_kind`, `resource_attributes`, `scope_attributes`, `point_attributes`) PARTITION BY (toYYYYMM(bucket)) TTL bucket + toIntervalDay(90) SETTINGS index_granularity = 8192 COMMENT 'Delivery-weighted histogram observations without buckets or quantiles. Mean is total sum divided by total count. No automatic zeros for missing minutes';
-- Create "product_metric_sums_1m" table
CREATE TABLE `gram`.`product_metric_sums_1m` (
  `organization_id` String,
  `project_id` UUID,
  `metric_name` String,
  `bucket` DateTime('UTC'),
  `scope_name` String,
  `scope_version` String,
  `unit` String,
  `number_kind` LowCardinality(String),
  `resource_attributes` Array(Tuple(`key` String, `type` String, `value` String)),
  `scope_attributes` Array(Tuple(`key` String, `type` String, `value` String)),
  `point_attributes` Array(Tuple(`key` String, `type` String, `value` String)),
  `integer_sum` SimpleAggregateFunction(sum, Int128),
  `floating_sum` SimpleAggregateFunction(sum, Float64),
  `contributions` SimpleAggregateFunction(sum, UInt64)
) ENGINE = AggregatingMergeTree
PRIMARY KEY (`organization_id`, `project_id`, `metric_name`, `bucket`, `scope_name`, `scope_version`, `unit`, `number_kind`, `resource_attributes`, `scope_attributes`, `point_attributes`) ORDER BY (`organization_id`, `project_id`, `metric_name`, `bucket`, `scope_name`, `scope_version`, `unit`, `number_kind`, `resource_attributes`, `scope_attributes`, `point_attributes`) PARTITION BY (toYYYYMM(bucket)) TTL bucket + toIntervalDay(90) SETTINGS index_granularity = 8192 COMMENT 'Monotonic delta sums over half-open UTC minute windows. Explicit sum reads are correct before merges. Late arrivals update retained buckets and current windows are partial';
-- Create "product_metric_histograms_1m_mv" view
CREATE MATERIALIZED VIEW `gram`.`product_metric_histograms_1m_mv` TO `gram`.`product_metric_histograms_1m` AS SELECT organization_id, project_id, metric_name, toStartOfMinute(event_time) AS bucket, scope_name, scope_version, unit, number_kind, resource_attributes, scope_attributes, point_attributes, sum(toInt128(integer_value)) AS integer_sum, sum(floating_value) AS floating_sum, count() AS contributions, min(integer_value) AS integer_min, max(integer_value) AS integer_max, min(floating_value) AS floating_min, max(floating_value) AS floating_max FROM gram.product_metric_contributions WHERE instrument = 'histogram' GROUP BY organization_id, project_id, metric_name, bucket, scope_name, scope_version, unit, number_kind, resource_attributes, scope_attributes, point_attributes;
-- Create "product_metric_sums_1m_mv" view
CREATE MATERIALIZED VIEW `gram`.`product_metric_sums_1m_mv` TO `gram`.`product_metric_sums_1m` AS SELECT organization_id, project_id, metric_name, toStartOfMinute(event_time) AS bucket, scope_name, scope_version, unit, number_kind, resource_attributes, scope_attributes, point_attributes, sum(toInt128(integer_value)) AS integer_sum, sum(floating_value) AS floating_sum, count() AS contributions FROM gram.product_metric_contributions WHERE instrument = 'counter' GROUP BY organization_id, project_id, metric_name, bucket, scope_name, scope_version, unit, number_kind, resource_attributes, scope_attributes, point_attributes;
