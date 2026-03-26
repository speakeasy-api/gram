ALTER TABLE `telemetry_logs` ADD INDEX `idx_telemetry_logs_mat_evaluation_score_label` ((evaluation_score_label)) TYPE bloom_filter(0.01) GRANULARITY 1;
