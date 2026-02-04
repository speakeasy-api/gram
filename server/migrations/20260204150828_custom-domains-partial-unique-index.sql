-- atlas:txmode none

-- Drop index "custom_domains_domain_key" from table: "custom_domains"
DROP INDEX CONCURRENTLY IF EXISTS "custom_domains_domain_key";
-- Create index "custom_domains_domain_key" to table: "custom_domains"
CREATE UNIQUE INDEX CONCURRENTLY "custom_domains_domain_key" ON "custom_domains" ("domain") WHERE (deleted IS FALSE);
