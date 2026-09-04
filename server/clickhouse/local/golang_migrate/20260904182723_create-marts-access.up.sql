-- Create the view-only marts database.
CREATE DATABASE `marts` ENGINE Atomic;
-- Create the constrained reader role.
CREATE ROLE IF NOT EXISTS `marts_reader` SETTINGS
  `readonly` = 1 CONST,
  `max_execution_time` = 30 CONST,
  `max_memory_usage` = 2000000000 CONST,
  `max_rows_to_read` = 100000000 CONST,
  `max_bytes_to_read` = 5000000000 CONST,
  `max_threads` = 4 CONST,
  `max_result_rows` = 10000 CONST,
  `max_result_bytes` = 10000000 CONST,
  `result_overflow_mode` = 'throw' CONST,
  `max_concurrent_queries_for_user` = 4 CONST;
-- Create a non-login principal with only the source access required by the views.
CREATE USER IF NOT EXISTS `marts_definer` HOST NONE;
GRANT SELECT ON `default`.`attribute_metrics_summaries` TO `marts_definer`;
-- Grant access to present and future approved mart views.
GRANT SELECT ON `marts`.* TO `marts_reader`;