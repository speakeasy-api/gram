ALTER TABLE `telemetry_logs` ADD COLUMN `mcp_server_id` String MATERIALIZED toString(attributes.gram.mcp_server.id) COMMENT 'MCP server ID (materialized from attributes.gram.mcp_server.id).';
ALTER TABLE `telemetry_logs` ADD INDEX `idx_telemetry_logs_mat_mcp_server_id` ((mcp_server_id)) TYPE bloom_filter(0.01) GRANULARITY 1;
