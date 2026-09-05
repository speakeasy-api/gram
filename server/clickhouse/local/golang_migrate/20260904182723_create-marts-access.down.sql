REVOKE SELECT ON `marts`.* FROM `marts_reader`;
REVOKE SELECT ON `gram`.`attribute_metrics_summaries` FROM `marts_definer`;
DROP USER `marts_definer`;
DROP ROLE `marts_reader`;
DROP DATABASE `marts`;