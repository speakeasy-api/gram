ALTER TABLE `telemetry_logs` ADD COLUMN `toolset_slug` String MATERIALIZED toString(attributes.gram.toolset.slug) COMMENT 'Toolset slug (materialized from attributes.gram.toolset.slug).';
ALTER TABLE `telemetry_logs` ADD INDEX `idx_telemetry_logs_mat_toolset_slug` ((toolset_slug)) TYPE bloom_filter(0.01) GRANULARITY 1;
