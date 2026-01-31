ALTER TABLE `telemetry_logs` ADD INDEX `idx_telemetry_logs_chat_id` ((gram_chat_id)) TYPE bloom_filter(0.01) GRANULARITY 1;
