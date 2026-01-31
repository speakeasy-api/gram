ALTER TABLE `telemetry_logs` ADD COLUMN `http_route` Nullable(String) COMMENT 'HTTP route pattern (/api/v1/users) - null for non-HTTP logs.';
