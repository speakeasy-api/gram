ALTER TABLE `telemetry_logs` ADD COLUMN `chat_id` Nullable(UUID) COMMENT 'Chat session ID for correlating completions, tool calls, and function runs.';
ALTER TABLE `telemetry_logs` ADD INDEX `idx_telemetry_logs_chat_id` ((chat_id)) TYPE bloom_filter(0.01) GRANULARITY 1;
