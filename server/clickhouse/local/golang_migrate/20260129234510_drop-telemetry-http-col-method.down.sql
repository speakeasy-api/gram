ALTER TABLE `telemetry_logs` ADD COLUMN `http_request_method` LowCardinality(Nullable(String)) COMMENT 'HTTP method (GET, POST, etc.) - null for non-HTTP logs.';
