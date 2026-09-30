-- Pause writers. Abort before changing objects if identities cannot be restored.
SELECT throwIf(count() > 0, 'repair catalogue identities before rollback') FROM (
 SELECT series_id FROM gram.product_metric_series GROUP BY series_id
 HAVING uniqExact(tuple(organization_id, project_id, metric_name, scope_name, scope_version, unit, instrument, number_kind, resource_attributes, scope_attributes, point_attributes)) != 1
);
SELECT throwIf(count() > 0, 'repair missing catalogue entries before rollback') FROM (
 SELECT series_id FROM gram.product_metric_sums_1m UNION ALL SELECT series_id FROM gram.product_metric_histograms_1m
) WHERE series_id NOT IN (SELECT series_id FROM gram.product_metric_series);
DROP VIEW gram.product_metric_sums_1m_mv;
DROP VIEW gram.product_metric_sums_1h_mv;
DROP VIEW gram.product_metric_sums_1d_mv;
DROP VIEW gram.product_metric_histograms_1m_mv;
DROP VIEW gram.product_metric_histograms_1h_mv;
DROP VIEW gram.product_metric_histograms_1d_mv;
DROP VIEW gram.product_metric_series_mv;
RENAME TABLE gram.product_metric_sums_1m TO gram.product_metric_sums_1m_tmp;
RENAME TABLE gram.product_metric_histograms_1m TO gram.product_metric_histograms_1m_tmp;
CREATE TABLE gram.product_metric_sums_1m (
 organization_id String, project_id UUID, metric_name String, bucket DateTime('UTC'), scope_name String, scope_version String, unit String, number_kind LowCardinality(String),
 resource_attributes Array(Tuple(key String, type String, value String)), scope_attributes Array(Tuple(key String, type String, value String)), point_attributes Array(Tuple(key String, type String, value String)),
 integer_sum SimpleAggregateFunction(sum, Int128), floating_sum SimpleAggregateFunction(sum, Float64), contributions SimpleAggregateFunction(sum, UInt64)
) ENGINE = AggregatingMergeTree PARTITION BY toYYYYMM(bucket)
ORDER BY (organization_id, project_id, metric_name, bucket, scope_name, scope_version, unit, number_kind, resource_attributes, scope_attributes, point_attributes)
TTL bucket + INTERVAL 90 DAY
COMMENT 'Monotonic delta sums over half-open UTC minute windows. Explicit sum reads are correct before merges. Late arrivals update retained buckets and current windows are partial';
CREATE TABLE gram.product_metric_histograms_1m (
 organization_id String, project_id UUID, metric_name String, bucket DateTime('UTC'), scope_name String, scope_version String, unit String, number_kind LowCardinality(String),
 resource_attributes Array(Tuple(key String, type String, value String)), scope_attributes Array(Tuple(key String, type String, value String)), point_attributes Array(Tuple(key String, type String, value String)),
 integer_sum SimpleAggregateFunction(sum, Int128), floating_sum SimpleAggregateFunction(sum, Float64), contributions SimpleAggregateFunction(sum, UInt64),
 integer_min SimpleAggregateFunction(min, Int64), integer_max SimpleAggregateFunction(max, Int64), floating_min SimpleAggregateFunction(min, Float64), floating_max SimpleAggregateFunction(max, Float64)
) ENGINE = AggregatingMergeTree PARTITION BY toYYYYMM(bucket)
ORDER BY (organization_id, project_id, metric_name, bucket, scope_name, scope_version, unit, number_kind, resource_attributes, scope_attributes, point_attributes)
TTL bucket + INTERVAL 90 DAY
COMMENT 'Delivery-weighted histogram observations without buckets or quantiles. Mean is total sum divided by total count. No automatic zeros for missing minutes';
INSERT INTO gram.product_metric_sums_1m SELECT p.organization_id, p.project_id, p.metric_name, p.bucket, p.scope_name, p.scope_version, p.unit, p.number_kind,
 c.resource_attributes, c.scope_attributes, c.point_attributes, p.integer_sum, p.floating_sum, p.contributions
 FROM gram.product_metric_sums_1m_tmp p INNER JOIN
 (SELECT series_id, any(resource_attributes) AS resource_attributes, any(scope_attributes) AS scope_attributes, any(point_attributes) AS point_attributes FROM gram.product_metric_series GROUP BY series_id) c USING series_id SETTINGS async_insert=0;
INSERT INTO gram.product_metric_histograms_1m SELECT p.organization_id, p.project_id, p.metric_name, p.bucket, p.scope_name, p.scope_version, p.unit, p.number_kind,
 c.resource_attributes, c.scope_attributes, c.point_attributes, p.integer_sum, p.floating_sum, p.contributions, p.integer_min, p.integer_max, p.floating_min, p.floating_max
 FROM gram.product_metric_histograms_1m_tmp p INNER JOIN
 (SELECT series_id, any(resource_attributes) AS resource_attributes, any(scope_attributes) AS scope_attributes, any(point_attributes) AS point_attributes FROM gram.product_metric_series GROUP BY series_id) c USING series_id SETTINGS async_insert=0;
DROP TABLE gram.product_metric_sums_1m_tmp;
DROP TABLE gram.product_metric_histograms_1m_tmp;
DROP TABLE gram.product_metric_sums_1h;
DROP TABLE gram.product_metric_sums_1d;
DROP TABLE gram.product_metric_histograms_1h;
DROP TABLE gram.product_metric_histograms_1d;
DROP TABLE gram.product_metric_series;
ALTER TABLE gram.product_metric_contributions DROP COLUMN series_id;
CREATE MATERIALIZED VIEW gram.product_metric_sums_1m_mv TO gram.product_metric_sums_1m AS
SELECT organization_id, project_id, metric_name, toStartOfMinute(event_time) AS bucket, scope_name, scope_version, unit, number_kind, resource_attributes, scope_attributes, point_attributes,
 sum(toInt128(integer_value)) AS integer_sum, sum(floating_value) AS floating_sum, count() AS contributions
FROM gram.product_metric_contributions WHERE instrument = 'counter'
GROUP BY organization_id, project_id, metric_name, bucket, scope_name, scope_version, unit, number_kind, resource_attributes, scope_attributes, point_attributes;
CREATE MATERIALIZED VIEW gram.product_metric_histograms_1m_mv TO gram.product_metric_histograms_1m AS
SELECT organization_id, project_id, metric_name, toStartOfMinute(event_time) AS bucket, scope_name, scope_version, unit, number_kind, resource_attributes, scope_attributes, point_attributes,
 sum(toInt128(integer_value)) AS integer_sum, sum(floating_value) AS floating_sum, count() AS contributions,
 min(integer_value) AS integer_min, max(integer_value) AS integer_max, min(floating_value) AS floating_min, max(floating_value) AS floating_max
FROM gram.product_metric_contributions WHERE instrument = 'histogram'
GROUP BY organization_id, project_id, metric_name, bucket, scope_name, scope_version, unit, number_kind, resource_attributes, scope_attributes, point_attributes;
