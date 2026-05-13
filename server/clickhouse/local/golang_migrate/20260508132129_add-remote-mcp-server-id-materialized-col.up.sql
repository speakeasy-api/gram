ALTER TABLE `telemetry_logs` ADD COLUMN `remote_mcp_server_id` String MATERIALIZED toString(attributes.gram.remote_mcp_server.id) COMMENT 'Remote MCP server ID (materialized from attributes.gram.remote_mcp_server.id).';
ALTER TABLE `telemetry_logs` ADD INDEX `idx_telemetry_logs_mat_remote_mcp_server_id` ((remote_mcp_server_id)) TYPE bloom_filter(0.01) GRANULARITY 1;
