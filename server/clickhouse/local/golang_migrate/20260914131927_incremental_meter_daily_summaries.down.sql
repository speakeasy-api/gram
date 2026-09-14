-- reverse: create "billing_meter_daily_summaries_mv" view
DROP VIEW `gram`.`billing_meter_daily_summaries_mv`;
-- reverse: create "billing_meter_daily_summaries" table
DROP TABLE `gram`.`billing_meter_daily_summaries`;
ALTER TABLE `gram`.`billing_meter_readings_by_time` MODIFY COMMENT 'Time-windowed usage ledger with redelivery convergence requiring FINAL before aggregation';
