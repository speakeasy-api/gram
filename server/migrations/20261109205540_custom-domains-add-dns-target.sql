-- atlas:txmode none

-- Modify "custom_domains" table
ALTER TABLE "custom_domains" ADD CONSTRAINT "custom_domains_dns_target_token_check" CHECK ((dns_target_token IS NULL) OR ((dns_target_token <> ''::text) AND (char_length(dns_target_token) <= 63))), ADD COLUMN "dns_target_token" text NULL, ADD COLUMN "dns_routing_method" text NULL;
-- Create index "custom_domains_dns_target_token_key" to table: "custom_domains"
CREATE UNIQUE INDEX CONCURRENTLY "custom_domains_dns_target_token_key" ON "custom_domains" ("dns_target_token") WHERE (deleted IS FALSE);
-- Set comment to column: "dns_target_token" on table: "custom_domains"
COMMENT ON COLUMN "custom_domains"."dns_target_token" IS '@access: confidential';
-- Set comment to column: "dns_routing_method" on table: "custom_domains"
COMMENT ON COLUMN "custom_domains"."dns_routing_method" IS '@access: confidential';
