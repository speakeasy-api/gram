-- Modify "custom_domains" table
ALTER TABLE "custom_domains" ADD COLUMN "dns_configuration_status" text NULL, ADD COLUMN "dns_checked_at" timestamptz NULL;
-- Set comment to column: "dns_configuration_status" on table: "custom_domains"
COMMENT ON COLUMN "custom_domains"."dns_configuration_status" IS 'Advisory DNS configuration observation, independent of ownership and infrastructure health.
@access: confidential';
-- Set comment to column: "dns_checked_at" on table: "custom_domains"
COMMENT ON COLUMN "custom_domains"."dns_checked_at" IS 'Time of the DNS observation; matches health_checked_at only when both observations came from the same check.
@access: confidential';
