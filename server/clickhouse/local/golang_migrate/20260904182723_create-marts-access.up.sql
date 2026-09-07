-- The marts database, marts_definer user, and marts_reader role are provisioned
-- externally by Terraform before migrations run.
GRANT SELECT ON `gram`.`attribute_metrics_summaries` TO `marts_definer`;