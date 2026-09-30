-- Run only in a disposable migrated database. This clears synthetic metrics data.
-- Pass --param_repair_partition=<YYYYMM for UTC now minus two minutes>.
SET async_insert = 0, materialized_views_ignore_errors = 0, insert_deduplicate = 0;
TRUNCATE TABLE product_metric_contributions;
TRUNCATE TABLE product_metric_sums_1m;
TRUNCATE TABLE product_metric_histograms_1m;
SYSTEM STOP MERGES product_metric_contributions;
SYSTEM STOP MERGES product_metric_sums_1m;
SYSTEM STOP MERGES product_metric_histograms_1m;

INSERT INTO product_metric_contributions
    (organization_id, project_id, metric_name, scope_name, unit, instrument, number_kind, contribution_id, event_time, integer_value, point_attributes, ingested_at)
SELECT 'synthetic-org', toUUID('00000000-0000-4000-8000-000000000001'), 'gram.synthetic.count',
    'gram.synthetic', '{measurement}', 'counter', 'integer', 'same-observation',
    toStartOfMinute(now()) - INTERVAL 2 MINUTE, toInt64('9223372036854775807'), [('dimension', 'STRING', '"value"')], now64(9) - INTERVAL 1 DAY;
-- Duplicate in a separate insert must contribute again, including before merges.
INSERT INTO product_metric_contributions SELECT * REPLACE (now64(9) AS ingested_at) FROM product_metric_contributions;
SELECT throwIf(toString(sum(integer_sum)) != '18446744073709551614' OR sum(contributions) != 2,
    'counter duplicates or integer precision lost') FROM product_metric_sums_1m
WHERE organization_id = 'synthetic-org' AND project_id = '00000000-0000-4000-8000-000000000001' AND metric_name = 'gram.synthetic.count';
SELECT throwIf(count() != 2, 'raw delivery copies were not retained before merges') FROM product_metric_contributions;
-- Different ingestion days still share a partition, selected by immutable event time.
SELECT throwIf(count() != 1 OR sum(integer_value) != toInt128('9223372036854775807') OR min(ingested_at) < now() - INTERVAL 1 HOUR,
    'deduplicated raw identity or latest delivery selection incorrect') FROM product_metric_contributions FINAL;

-- Same metric, different tenant/project/resource/scope/type/missing-empty dimensions.
INSERT INTO product_metric_contributions
    (organization_id, project_id, metric_name, scope_name, scope_attributes, resource_attributes, point_attributes, instrument, number_kind, event_time, integer_value, contribution_id)
SELECT if(number = 0, 'other-synthetic-org', 'synthetic-org'),
    toUUID(if(number = 1, '00000000-0000-4000-8000-000000000002', '00000000-0000-4000-8000-000000000001')),
    'gram.synthetic.identity', 'gram.synthetic',
    if(number = 2, [('key', 'STRING', '""')], []),
    if(number = 3, [('key', 'STRING', '""')], []),
    multiIf(number = 4, [('key', 'STRING', '""')], number = 5, [('key', 'INT64', '1')], number = 6, [('key', 'DOUBLE', '1')], []),
    'counter', 'integer', toStartOfMinute(now()) - INTERVAL 2 MINUTE, 1, concat('identity-', toString(number))
FROM numbers(8);
SELECT throwIf(count() != 8, 'series identities collapsed') FROM product_metric_sums_1m WHERE metric_name = 'gram.synthetic.identity';

INSERT INTO product_metric_contributions
    (organization_id, project_id, metric_name, instrument, number_kind, event_time, floating_value, contribution_id)
SELECT 'synthetic-org', toUUID('00000000-0000-4000-8000-000000000001'), 'gram.synthetic.duration',
    'histogram', 'floating', toStartOfMinute(now()) - toIntervalMinute(if(number = 0, 2, 1)),
    if(number = 0, 10., 1.), concat('histogram-', toString(number)) FROM numbers(10);
SELECT throwIf(sum(contributions) != 10 OR sum(floating_sum) != 19 OR min(floating_min) != 1 OR max(floating_max) != 10 OR sum(floating_sum)/sum(contributions) != 1.9,
    'histogram regrouping or weighted mean incorrect') FROM product_metric_histograms_1m
WHERE organization_id = 'synthetic-org' AND project_id = '00000000-0000-4000-8000-000000000001' AND metric_name = 'gram.synthetic.duration';

-- Replacement identity includes tenant/project and instrumentation scope/version.
-- Distinct observation IDs sharing all other fields must also survive replacement.
INSERT INTO product_metric_contributions
    (organization_id, project_id, metric_name, scope_name, scope_version, contribution_id, event_time, instrument, number_kind, integer_value)
SELECT if(number = 0, 'other-synthetic-org', 'synthetic-org'),
    toUUID(if(number = 1, '00000000-0000-4000-8000-000000000002', '00000000-0000-4000-8000-000000000001')),
    'gram.synthetic.raw_identity', if(number = 2, 'other.scope', 'gram.synthetic'), if(number = 3, '2', '1'),
    if(number = 5, 'distinct-observation', 'shared-observation'), toStartOfMinute(now()) - INTERVAL 2 MINUTE,
    'counter', 'integer', 1 FROM numbers(6);
SELECT throwIf(count() != 6, 'raw replacement collapsed independent observations') FROM product_metric_contributions FINAL WHERE metric_name = 'gram.synthetic.raw_identity';

-- A repeated high histogram value biases the live delivery-weighted mean.
INSERT INTO product_metric_contributions SELECT * REPLACE (now64(9) AS ingested_at) FROM product_metric_contributions
WHERE metric_name = 'gram.synthetic.duration' AND contribution_id = 'histogram-0';
SELECT throwIf(sum(contributions) != 11 OR sum(floating_sum) != 29, 'histogram retry was not duplicate-inclusive')
FROM product_metric_histograms_1m WHERE metric_name = 'gram.synthetic.duration';

-- Both raw and rollup TTLs use event-minute age, not ingestion age.
INSERT INTO product_metric_contributions
    (organization_id, project_id, metric_name, scope_name, contribution_id, event_time, ingested_at, instrument, number_kind, integer_value)
SELECT 'synthetic-org', toUUID('00000000-0000-4000-8000-000000000001'), 'gram.synthetic.retention', 'gram.synthetic',
    if(number = 0, 'retained', 'expired'), toStartOfMinute(now()) - toIntervalDay(if(number = 0, 89, 91)),
    now64(9), 'counter', 'integer', 1 FROM numbers(2);

SYSTEM START MERGES product_metric_contributions;
OPTIMIZE TABLE product_metric_contributions FINAL;
ALTER TABLE product_metric_contributions MATERIALIZE TTL SETTINGS mutations_sync = 2;
SELECT throwIf(count() != 1, 'raw copies were not replaced after merges') FROM product_metric_contributions WHERE metric_name = 'gram.synthetic.count';
SELECT throwIf(count() != 1 OR min(contribution_id) != 'retained', 'raw retention is not 90 event-time days') FROM product_metric_contributions WHERE metric_name = 'gram.synthetic.retention';
SELECT throwIf(count() != 6, 'raw replacement key lost identity after merges') FROM product_metric_contributions WHERE metric_name = 'gram.synthetic.raw_identity';
SELECT throwIf(sum(contributions) != 2, 'raw replacement unexpectedly changed counter rollups') FROM product_metric_sums_1m WHERE metric_name = 'gram.synthetic.count';
SELECT throwIf(sum(contributions) != 11 OR sum(floating_sum) != 29, 'raw replacement unexpectedly changed histogram rollups') FROM product_metric_histograms_1m WHERE metric_name = 'gram.synthetic.duration';

-- Exercise the manual repair primitive with writers quiesced: aggregate FINAL raw
-- rows into isolated targets, then REPLACE the complete affected event partition.
-- Never replay raw rows into the live source or append rebuilt totals to rollups.
DROP TABLE IF EXISTS product_metric_sums_rebuilt;
DROP TABLE IF EXISTS product_metric_histograms_rebuilt;
CREATE TABLE product_metric_sums_rebuilt AS product_metric_sums_1m;
CREATE TABLE product_metric_histograms_rebuilt AS product_metric_histograms_1m;
INSERT INTO product_metric_sums_rebuilt
SELECT organization_id, project_id, metric_name, toStartOfMinute(event_time) AS bucket,
    scope_name, scope_version, unit, number_kind, resource_attributes, scope_attributes, point_attributes,
    sum(toInt128(integer_value)), sum(floating_value), count()
FROM product_metric_contributions FINAL WHERE instrument = 'counter' AND event_time >= now() - INTERVAL 90 DAY
GROUP BY organization_id, project_id, metric_name, bucket, scope_name, scope_version, unit,
    number_kind, resource_attributes, scope_attributes, point_attributes;
INSERT INTO product_metric_histograms_rebuilt
SELECT organization_id, project_id, metric_name, toStartOfMinute(event_time) AS bucket,
    scope_name, scope_version, unit, number_kind, resource_attributes, scope_attributes, point_attributes,
    sum(toInt128(integer_value)), sum(floating_value), count(), min(integer_value), max(integer_value), min(floating_value), max(floating_value)
FROM product_metric_contributions FINAL WHERE instrument = 'histogram' AND event_time >= now() - INTERVAL 90 DAY
GROUP BY organization_id, project_id, metric_name, bucket, scope_name, scope_version, unit,
    number_kind, resource_attributes, scope_attributes, point_attributes;
ALTER TABLE product_metric_sums_1m REPLACE PARTITION {repair_partition:UInt32} FROM product_metric_sums_rebuilt;
ALTER TABLE product_metric_histograms_1m REPLACE PARTITION {repair_partition:UInt32} FROM product_metric_histograms_rebuilt;
SELECT throwIf(toString(sum(integer_sum)) != '9223372036854775807' OR sum(contributions) != 1, 'manual counter repair failed') FROM product_metric_sums_1m WHERE metric_name = 'gram.synthetic.count';
SELECT throwIf(sum(contributions) != 10 OR sum(floating_sum) != 19 OR sum(floating_sum)/sum(contributions) != 1.9, 'manual histogram repair failed') FROM product_metric_histograms_1m WHERE metric_name = 'gram.synthetic.duration';
DROP TABLE product_metric_sums_rebuilt;
DROP TABLE product_metric_histograms_rebuilt;

SYSTEM START MERGES product_metric_sums_1m;
SYSTEM START MERGES product_metric_histograms_1m;
OPTIMIZE TABLE product_metric_sums_1m FINAL;
SELECT throwIf(count() != 8, 'merge key lost identity') FROM product_metric_sums_1m WHERE metric_name = 'gram.synthetic.identity';
SELECT 'product metrics rollups verified';
