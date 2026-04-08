ALTER TABLE `telemetry_logs` ADD COLUMN `user_email` String MATERIALIZED toString(attributes.user.email) COMMENT 'User email (materialized from attributes.user.email).';
ALTER TABLE `telemetry_logs` ADD COLUMN `hook_source` String MATERIALIZED toString(attributes.gram.hook.source) COMMENT 'Hook source (materialized from attributes.gram.hook.source).';
ALTER TABLE `telemetry_logs` ADD INDEX `idx_telemetry_logs_mat_hook_source` ((hook_source)) TYPE bloom_filter(0.01) GRANULARITY 1;
ALTER TABLE `telemetry_logs` ADD INDEX `idx_telemetry_logs_mat_user_email` ((user_email)) TYPE bloom_filter(0.01) GRANULARITY 1;
