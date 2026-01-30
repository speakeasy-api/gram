ALTER TABLE `telemetry_logs` ADD INDEX `idx_telemetry_logs_http_status` ((http_response_status_code)) TYPE set(100) GRANULARITY 4;
ALTER TABLE `telemetry_logs` ADD INDEX `idx_telemetry_logs_http_route` ((http_route)) TYPE bloom_filter(0.01) GRANULARITY 1;
