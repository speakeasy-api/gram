ALTER TABLE `telemetry_logs` DROP COLUMN `http_request_method`;
ALTER TABLE `telemetry_logs` DROP COLUMN `http_response_status_code`;
ALTER TABLE `telemetry_logs` DROP COLUMN `http_route`;
ALTER TABLE `telemetry_logs` DROP COLUMN `http_server_url`;
ALTER TABLE `telemetry_logs` ADD COLUMN `timestamp` DateTime64(9) DEFAULT fromUnixTimestamp64Nano(time_unix_nano) COMMENT 'Event timestamp derived from time_unix_nano for human-readable queries.';
ALTER TABLE `telemetry_logs` ADD COLUMN `gram_chat_id` Nullable(UUID) COMMENT 'Chat ID that triggered this log (null for non-chat contexts).';
ALTER TABLE `telemetry_logs` DROP INDEX `idx_telemetry_logs_http_route`;
ALTER TABLE `telemetry_logs` DROP INDEX `idx_telemetry_logs_http_status`;
ALTER TABLE `telemetry_logs` ADD INDEX `idx_telemetry_logs_chat_id` ((gram_chat_id)) TYPE bloom_filter(0.01) GRANULARITY 1;
