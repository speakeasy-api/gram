ALTER TABLE `http_requests_raw` ADD INDEX `idx_tool_urn_substring` ((tool_urn)) TYPE ngrambf_v1(4, 30720, 3, 0) GRANULARITY 4;
ALTER TABLE `http_requests_raw` ADD INDEX `idx_tool_urn_exact` ((tool_urn)) TYPE bloom_filter(0.01) GRANULARITY 4;
ALTER TABLE `http_requests_raw` ADD INDEX `idx_tool_type` ((tool_type)) TYPE set(0) GRANULARITY 4;
ALTER TABLE `http_requests_raw` ADD INDEX `idx_status_code` ((status_code)) TYPE set(100) GRANULARITY 4;
