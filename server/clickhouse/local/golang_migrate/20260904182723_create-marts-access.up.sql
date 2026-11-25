-- Database and principals are provisioned by Terraform in Cloud and bootstrap locally.
GRANT SELECT ON `gram`.`attribute_metrics_summaries` TO `marts_definer`;
