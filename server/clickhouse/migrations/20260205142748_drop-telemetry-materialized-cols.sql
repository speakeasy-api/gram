-- atlas:nolint destructive


-- Intentionally dropping materialized columns to recreate them with corrected expressions.
-- These columns are not used in production code yet, so no data loss impact.
ALTER TABLE `telemetry_logs` DROP COLUMN `project_id`;
ALTER TABLE `telemetry_logs` DROP COLUMN `deployment_id`;
ALTER TABLE `telemetry_logs` DROP COLUMN `function_id`;
ALTER TABLE `telemetry_logs` DROP COLUMN `urn`;
ALTER TABLE `telemetry_logs` DROP COLUMN `chat_id`;
ALTER TABLE `telemetry_logs` DROP COLUMN `user_id`;
ALTER TABLE `telemetry_logs` DROP COLUMN `external_user_id`;
