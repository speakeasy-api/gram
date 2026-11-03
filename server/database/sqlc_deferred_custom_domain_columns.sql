-- SQLc-only schema overlay: keep the schema release's domain queries compatible
-- with databases where the additive migration has not applied yet. This file is
-- never applied to a database. AGE-3601 removes it when introducing column readers.
ALTER TABLE custom_domains DROP COLUMN dns_target_token;
ALTER TABLE custom_domains DROP COLUMN dns_routing_method;
ALTER TABLE custom_domains DROP COLUMN dns_configuration_status;
ALTER TABLE custom_domains DROP COLUMN dns_checked_at;
