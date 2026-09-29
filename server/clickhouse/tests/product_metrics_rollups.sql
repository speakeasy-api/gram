-- Run only in a disposable migrated database. This clears synthetic metrics data.
SET async_insert = 0, materialized_views_ignore_errors = 0, insert_deduplicate = 0;
TRUNCATE TABLE product_metric_contributions;
TRUNCATE TABLE product_metric_sums_1m;
TRUNCATE TABLE product_metric_histograms_1m;
SYSTEM STOP MERGES product_metric_sums_1m;
SYSTEM STOP MERGES product_metric_histograms_1m;

INSERT INTO product_metric_contributions
    (organization_id, project_id, metric_name, scope_name, unit, instrument, number_kind, contribution_id, event_time, integer_value, point_attributes)
SELECT 'synthetic-org', toUUID('00000000-0000-4000-8000-000000000001'), 'gram.synthetic.count',
    'gram.synthetic', '{measurement}', 'counter', 'integer', 'same-observation',
    toStartOfMinute(now()) - INTERVAL 2 MINUTE, toInt64('9223372036854775807'), [('dimension', 'STRING', '"value"')];
-- Duplicate in a separate insert must contribute again, including before merges.
INSERT INTO product_metric_contributions SELECT * FROM product_metric_contributions;
SELECT throwIf(toString(sum(integer_sum)) != '18446744073709551614' OR sum(contributions) != 2,
    'counter duplicates or integer precision lost') FROM product_metric_sums_1m
WHERE organization_id = 'synthetic-org' AND project_id = '00000000-0000-4000-8000-000000000001' AND metric_name = 'gram.synthetic.count';

-- Same metric, different tenant/project/resource/scope/type/missing-empty dimensions.
INSERT INTO product_metric_contributions
    (organization_id, project_id, metric_name, scope_name, scope_attributes, resource_attributes, point_attributes, instrument, number_kind, event_time, integer_value)
SELECT if(number = 0, 'other-synthetic-org', 'synthetic-org'),
    toUUID(if(number = 1, '00000000-0000-4000-8000-000000000002', '00000000-0000-4000-8000-000000000001')),
    'gram.synthetic.identity', 'gram.synthetic',
    if(number = 2, [('key', 'STRING', '""')], []),
    if(number = 3, [('key', 'STRING', '""')], []),
    multiIf(number = 4, [('key', 'STRING', '""')], number = 5, [('key', 'INT64', '1')], number = 6, [('key', 'DOUBLE', '1')], []),
    'counter', 'integer', toStartOfMinute(now()) - INTERVAL 2 MINUTE, 1
FROM numbers(8);
SELECT throwIf(count() != 8, 'series identities collapsed') FROM product_metric_sums_1m WHERE metric_name = 'gram.synthetic.identity';

INSERT INTO product_metric_contributions
    (organization_id, project_id, metric_name, instrument, number_kind, event_time, floating_value)
SELECT 'synthetic-org', toUUID('00000000-0000-4000-8000-000000000001'), 'gram.synthetic.duration',
    'histogram', 'floating', toStartOfMinute(now()) - toIntervalMinute(if(number = 0, 2, 1)),
    if(number = 0, 10., 1.) FROM numbers(10);
SELECT throwIf(sum(contributions) != 10 OR sum(floating_sum) != 19 OR min(floating_min) != 1 OR max(floating_max) != 10 OR sum(floating_sum)/sum(contributions) != 1.9,
    'histogram regrouping or weighted mean incorrect') FROM product_metric_histograms_1m
WHERE organization_id = 'synthetic-org' AND project_id = '00000000-0000-4000-8000-000000000001' AND metric_name = 'gram.synthetic.duration';

SYSTEM START MERGES product_metric_sums_1m;
SYSTEM START MERGES product_metric_histograms_1m;
OPTIMIZE TABLE product_metric_sums_1m FINAL;
SELECT throwIf(count() != 8, 'merge key lost identity') FROM product_metric_sums_1m WHERE metric_name = 'gram.synthetic.identity';
SELECT 'product metrics rollups verified';
