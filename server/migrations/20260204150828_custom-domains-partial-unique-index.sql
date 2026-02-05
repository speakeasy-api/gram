-- atlas:txmode none

-- Create new partial unique index under a temporary name
CREATE UNIQUE INDEX CONCURRENTLY "custom_domains_domain_key_new" ON "custom_domains" ("domain") WHERE (deleted IS FALSE);
-- Drop the old non-partial unique index
DROP INDEX CONCURRENTLY IF EXISTS "custom_domains_domain_key";
-- Rename new index to the original name
ALTER INDEX "custom_domains_domain_key_new" RENAME TO "custom_domains_domain_key";
