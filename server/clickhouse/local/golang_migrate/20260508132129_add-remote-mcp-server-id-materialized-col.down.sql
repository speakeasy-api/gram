ALTER TABLE `telemetry_logs` DROP INDEX `idx_telemetry_logs_mat_remote_mcp_server_id`;
ALTER TABLE `telemetry_logs` DROP COLUMN `remote_mcp_server_id`;
