ALTER TABLE `telemetry_logs` DROP INDEX `idx_telemetry_logs_chat_id`;
ALTER TABLE `telemetry_logs` DROP COLUMN `gram_chat_id`;
ALTER TABLE `telemetry_logs` DROP COLUMN `observed_timestamp`;
