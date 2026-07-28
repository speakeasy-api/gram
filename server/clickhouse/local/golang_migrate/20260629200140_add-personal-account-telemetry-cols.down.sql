ALTER TABLE `telemetry_logs` DROP INDEX `idx_telemetry_logs_mat_provider`;
ALTER TABLE `telemetry_logs` DROP INDEX `idx_telemetry_logs_mat_external_org_id`;
ALTER TABLE `telemetry_logs` DROP INDEX `idx_telemetry_logs_mat_account_type`;
ALTER TABLE `telemetry_logs` DROP COLUMN `account_type`;
ALTER TABLE `telemetry_logs` DROP COLUMN `external_org_id`;
ALTER TABLE `telemetry_logs` DROP COLUMN `provider`;
