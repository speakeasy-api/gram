ALTER TABLE `telemetry_logs` ADD COLUMN `http_server_url` Nullable(String) COMMENT 'HTTP server URL - null for non-HTTP logs.' CODEC(ZSTD(1));
ALTER TABLE `telemetry_logs` ADD COLUMN `http_route` Nullable(String) COMMENT 'HTTP route pattern (/api/v1/users) - null for non-HTTP logs.';
ALTER TABLE `telemetry_logs` ADD COLUMN `http_response_status_code` Nullable(Int32) COMMENT 'HTTP status code - null for non-HTTP logs.';
ALTER TABLE `telemetry_logs` ADD COLUMN `http_request_method` LowCardinality(Nullable(String)) COMMENT 'HTTP method (GET, POST, etc.) - null for non-HTTP logs.';
