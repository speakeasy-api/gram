ALTER TABLE `telemetry_logs` DROP INDEX `idx_telemetry_logs_mat_mcp_server_id`;
ALTER TABLE `telemetry_logs` DROP COLUMN `mcp_server_id`;
