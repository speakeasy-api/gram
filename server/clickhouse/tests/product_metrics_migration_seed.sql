-- Seed the original migration in a disposable database before the engine swap.
-- Repeated counter delivery and distinct histogram observations must survive the
-- copy without replaying their increments into the existing materialized views.
SET async_insert = 0, insert_deduplicate = 0;
INSERT INTO product_metric_contributions
    (organization_id, project_id, metric_name, scope_name, contribution_id, event_time, instrument, number_kind, integer_value)
SELECT 'synthetic-migration', toUUID('00000000-0000-4000-8000-000000000001'),
    'gram.synthetic.migration_counter', 'gram.synthetic', 'migration-counter',
    toStartOfMinute(now()), 'counter', 'integer', 3 FROM numbers(2);
INSERT INTO product_metric_contributions
    (organization_id, project_id, metric_name, scope_name, contribution_id, event_time, instrument, number_kind, floating_value)
SELECT 'synthetic-migration', toUUID('00000000-0000-4000-8000-000000000001'),
    'gram.synthetic.migration_histogram', 'gram.synthetic', concat('migration-histogram-', toString(number)),
    toStartOfMinute(now()), 'histogram', 'floating', (number + 1) * 2. FROM numbers(2);
