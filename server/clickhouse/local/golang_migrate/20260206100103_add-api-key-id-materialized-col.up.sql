ALTER TABLE `telemetry_logs` ADD COLUMN `api_key_id` String MATERIALIZED toString(attributes.gram.api_key.id) COMMENT 'API key ID (materialized from attributes.gram.api_key.id).';
ALTER TABLE `telemetry_logs` ADD INDEX `idx_telemetry_logs_mat_api_key_id` ((api_key_id)) TYPE bloom_filter(0.01) GRANULARITY 1;
