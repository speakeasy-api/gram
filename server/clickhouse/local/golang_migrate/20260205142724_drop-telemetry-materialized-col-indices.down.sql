ALTER TABLE `telemetry_logs` ADD INDEX `idx_telemetry_logs_user_id` ((user_id)) TYPE bloom_filter(0.01) GRANULARITY 1;
ALTER TABLE `telemetry_logs` ADD INDEX `idx_telemetry_logs_new_urn` ((urn)) TYPE bloom_filter(0.01) GRANULARITY 1;
ALTER TABLE `telemetry_logs` ADD INDEX `idx_telemetry_logs_new_function_id` ((function_id)) TYPE bloom_filter(0.01) GRANULARITY 1;
ALTER TABLE `telemetry_logs` ADD INDEX `idx_telemetry_logs_new_deployment_id` ((deployment_id)) TYPE bloom_filter(0.01) GRANULARITY 1;
ALTER TABLE `telemetry_logs` ADD INDEX `idx_telemetry_logs_new_chat_id` ((chat_id)) TYPE bloom_filter(0.01) GRANULARITY 1;
ALTER TABLE `telemetry_logs` ADD INDEX `idx_telemetry_logs_external_user_id` ((external_user_id)) TYPE bloom_filter(0.01) GRANULARITY 1;
