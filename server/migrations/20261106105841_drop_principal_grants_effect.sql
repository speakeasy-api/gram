-- atlas:txmode none

-- Drop index "principal_grants_org_principal_scope_effect_selector_key" from table: "principal_grants"
DROP INDEX CONCURRENTLY "principal_grants_org_principal_scope_effect_selector_key";
-- Modify "principal_grants" table
ALTER TABLE "principal_grants" DROP CONSTRAINT "principal_grants_effect_check", DROP COLUMN "effect";
