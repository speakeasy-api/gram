-- Run after each upgrade/downgrade in the disposable, seeded migration database.
-- Pass --param_expected_engine=ReplacingMergeTree (up) or MergeTree (down).
SELECT throwIf(engine != {expected_engine:String}, 'wrong source engine after migration') FROM system.tables
WHERE database = currentDatabase() AND name = 'product_metric_contributions';
SELECT throwIf(uniqExact(tuple(metric_name, contribution_id)) != 3, 'migration lost retained observations')
FROM product_metric_contributions WHERE organization_id = 'synthetic-migration' AND metric_name != 'gram.synthetic.migration_probe';
SELECT throwIf(sum(contributions) != 2 OR sum(integer_sum) != 6, 'migration replayed or lost counter increments')
FROM product_metric_sums_1m WHERE organization_id = 'synthetic-migration' AND metric_name = 'gram.synthetic.migration_counter';
SELECT throwIf(sum(contributions) != 2 OR sum(floating_sum) != 6, 'migration replayed or lost histogram increments')
FROM product_metric_histograms_1m WHERE organization_id = 'synthetic-migration' AND metric_name = 'gram.synthetic.migration_histogram';

-- A fresh observation verifies the MV is attached to the replacement source.
INSERT INTO product_metric_contributions
    (organization_id, project_id, metric_name, scope_name, contribution_id, event_time, instrument, number_kind, integer_value)
SELECT 'synthetic-migration', toUUID('00000000-0000-4000-8000-000000000001'),
    'gram.synthetic.migration_probe', 'gram.synthetic', concat('probe-', toString(generateUUIDv4())),
    toStartOfMinute(now()), 'counter', 'integer', 1 SETTINGS async_insert = 0;
SELECT throwIf((SELECT sum(contributions) FROM product_metric_sums_1m WHERE metric_name = 'gram.synthetic.migration_probe') !=
    (SELECT uniqExact(contribution_id) FROM product_metric_contributions WHERE metric_name = 'gram.synthetic.migration_probe'),
    'materialized view is not attached after migration');
SELECT 'product metrics migration verified';
