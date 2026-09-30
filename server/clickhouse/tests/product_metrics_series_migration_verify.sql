-- Run after the series-tier migration on the seeded migration fixture database.
SELECT throwIf(count() > 0, 'rollup identities absent from catalogue') FROM (
 SELECT series_id FROM product_metric_sums_1m UNION ALL SELECT series_id FROM product_metric_histograms_1m
) WHERE series_id NOT IN (SELECT series_id FROM product_metric_series);
SELECT throwIf(count() > 0, 'migrated hash differs from raw materialized identity')
FROM product_metric_contributions WHERE series_id NOT IN (SELECT series_id FROM product_metric_series);
SELECT throwIf(sum(contributions) != 2 OR sum(integer_sum) != 6, 'hourly backfill changed delivery totals')
FROM product_metric_sums_1h WHERE metric_name = 'gram.synthetic.migration_counter';
SELECT throwIf(sum(contributions) != 2 OR sum(integer_sum) != 6, 'daily backfill changed delivery totals')
FROM product_metric_sums_1d WHERE metric_name = 'gram.synthetic.migration_counter';
SELECT throwIf(sum(contributions) != 2 OR sum(floating_sum) != 6, 'hourly histogram backfill changed delivery totals')
FROM product_metric_histograms_1h WHERE metric_name = 'gram.synthetic.migration_histogram';
SELECT throwIf(sum(contributions) != 2 OR sum(floating_sum) != 6, 'daily histogram backfill changed delivery totals')
FROM product_metric_histograms_1d WHERE metric_name = 'gram.synthetic.migration_histogram';
SELECT 'series migration verified';
