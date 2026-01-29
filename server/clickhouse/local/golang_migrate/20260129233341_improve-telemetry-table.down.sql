ALTER TABLE `telemetry_logs` DROP INDEX `idx_telemetry_logs_chat_id`;
ALTER TABLE `telemetry_logs` ADD INDEX `idx_telemetry_logs_http_status` ((http_response_status_code)) TYPE set(100) GRANULARITY 4;
ALTER TABLE `telemetry_logs` ADD INDEX `idx_telemetry_logs_http_route` ((http_route)) TYPE bloom_filter(0.01) GRANULARITY 1;
ALTER TABLE `telemetry_logs` DROP COLUMN `gram_chat_id`;
ALTER TABLE `telemetry_logs` DROP COLUMN `timestamp`;
ALTER TABLE `telemetry_logs` ADD COLUMN `http_server_url` Nullable(String) COMMENT 'HTTP server URL - null for non-HTTP logs.' CODEC(ZSTD(1));
ALTER TABLE `telemetry_logs` ADD COLUMN `http_route` Nullable(String) COMMENT 'HTTP route pattern (/api/v1/users) - null for non-HTTP logs.';
ALTER TABLE `telemetry_logs` ADD COLUMN `http_response_status_code` Nullable(Int32) COMMENT 'HTTP status code - null for non-HTTP logs.';
ALTER TABLE `telemetry_logs` ADD COLUMN `http_request_method` LowCardinality(Nullable(String)) COMMENT 'HTTP method (GET, POST, etc.) - null for non-HTTP logs.';
