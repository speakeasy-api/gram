-- atlas:txtar

-- checks/destructive.sql --
-- The legacy-policy-scope migration folded every value into per-category
-- detection scopes and validated an empty population in dev and production.
-- These assertions refuse the drop in any environment where that is not true,
-- which matters because the pre-fold values are no longer backed up anywhere.
-- atlas:assert DS103
SELECT NOT EXISTS (SELECT 1 FROM "risk_policies" WHERE "message_types" IS NOT NULL) AS "is_empty";

-- atlas:assert DS103
SELECT NOT EXISTS (SELECT 1 FROM "risk_policies" WHERE "scope_include" IS NOT NULL) AS "is_empty";

-- atlas:assert DS103
SELECT NOT EXISTS (SELECT 1 FROM "risk_policies" WHERE "scope_exempt" IS NOT NULL) AS "is_empty";

-- migration.sql --
-- Modify "risk_policies" table
ALTER TABLE "risk_policies" DROP COLUMN "message_types", DROP COLUMN "scope_include", DROP COLUMN "scope_exempt";
