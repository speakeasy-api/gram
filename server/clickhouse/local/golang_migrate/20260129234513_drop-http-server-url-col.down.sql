ALTER TABLE `telemetry_logs` ADD COLUMN `http_server_url` Nullable(String) COMMENT 'HTTP server URL - null for non-HTTP logs.' CODEC(ZSTD(1));
