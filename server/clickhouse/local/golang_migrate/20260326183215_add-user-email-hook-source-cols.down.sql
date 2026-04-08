ALTER TABLE `telemetry_logs` DROP INDEX `idx_telemetry_logs_mat_user_email`;
ALTER TABLE `telemetry_logs` DROP INDEX `idx_telemetry_logs_mat_hook_source`;
ALTER TABLE `telemetry_logs` DROP COLUMN `hook_source`;
ALTER TABLE `telemetry_logs` DROP COLUMN `user_email`;
