ALTER TABLE `tool_logs` ADD INDEX `idx_source` ((source)) TYPE set(0) GRANULARITY 4;
ALTER TABLE `tool_logs` ADD INDEX `idx_project_id` ((project_id)) TYPE bloom_filter(0.01) GRANULARITY 1;
ALTER TABLE `tool_logs` ADD INDEX `idx_level` ((level)) TYPE set(0) GRANULARITY 4;
ALTER TABLE `tool_logs` ADD INDEX `idx_instance` ((instance)) TYPE bloom_filter(0.01) GRANULARITY 1;
ALTER TABLE `tool_logs` ADD INDEX `idx_function_id` ((function_id)) TYPE bloom_filter(0.01) GRANULARITY 1;
ALTER TABLE `tool_logs` ADD INDEX `idx_deployment_id` ((deployment_id)) TYPE bloom_filter(0.01) GRANULARITY 1;
