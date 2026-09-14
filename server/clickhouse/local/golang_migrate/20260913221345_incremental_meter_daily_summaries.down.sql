-- reverse: create "billing_meter_daily_summaries_mv" view
DROP VIEW `gram`.`billing_meter_daily_summaries_mv`;
-- reverse: create "billing_meter_daily_summaries" table
DROP TABLE `gram`.`billing_meter_daily_summaries`;
ALTER TABLE `gram`.`billing_meter_readings_by_time`
  DROP COLUMN `tool_name`,
  DROP COLUMN `risk_policy_id`,
  DROP COLUMN `provider`,
  DROP COLUMN `model`,
  DROP COLUMN `mcp_server_type`,
  DROP COLUMN `mcp_server_slug`,
  DROP COLUMN `mcp_server_id`,
  DROP COLUMN `billing_user_job_title`,
  DROP COLUMN `billing_user_id`,
  DROP COLUMN `billing_user_employee_type`,
  DROP COLUMN `billing_user_division_name`,
  DROP COLUMN `billing_user_directory_groups`,
  DROP COLUMN `billing_user_department_name`,
  DROP COLUMN `billing_user_cost_center_name`,
  DROP COLUMN `billing_mode`,
  DROP COLUMN `assistant_id`,
  MODIFY COMMENT 'Time-windowed usage ledger with redelivery convergence requiring FINAL before aggregation';
