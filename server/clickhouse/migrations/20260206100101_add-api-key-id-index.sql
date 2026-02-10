ALTER TABLE `telemetry_logs` ADD INDEX `idx_telemetry_logs_mat_api_key_id` ((api_key_id)) TYPE bloom_filter(0.01) GRANULARITY 1;
