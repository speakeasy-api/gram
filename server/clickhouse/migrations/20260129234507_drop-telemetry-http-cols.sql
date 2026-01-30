-- atlas:nolint destructive
ALTER TABLE `telemetry_logs` DROP COLUMN `http_request_method`;
-- atlas:nolint destructive
ALTER TABLE `telemetry_logs` DROP COLUMN `http_response_status_code`;
-- atlas:nolint destructive
ALTER TABLE `telemetry_logs` DROP COLUMN `http_route`;
-- atlas:nolint destructive
ALTER TABLE `telemetry_logs` DROP COLUMN `http_server_url`;
