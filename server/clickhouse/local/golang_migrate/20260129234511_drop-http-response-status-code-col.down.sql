ALTER TABLE `telemetry_logs` ADD COLUMN `http_response_status_code` Nullable(Int32) COMMENT 'HTTP status code - null for non-HTTP logs.';
