-- atlas:txmode none

-- Drop index "skill_efficacy_evaluations_org_spend_idx" from table: "skill_efficacy_evaluations"
DROP INDEX CONCURRENTLY "skill_efficacy_evaluations_org_spend_idx";
-- Drop index "skill_efficacy_evaluations_skill_spend_idx" from table: "skill_efficacy_evaluations"
DROP INDEX CONCURRENTLY "skill_efficacy_evaluations_skill_spend_idx";
-- Drop index "skill_efficacy_evaluations_version_lifetime_spend_idx" from table: "skill_efficacy_evaluations"
DROP INDEX CONCURRENTLY "skill_efficacy_evaluations_version_lifetime_spend_idx";
-- Modify "skill_efficacy_evaluations" table
ALTER TABLE "skill_efficacy_evaluations" ADD COLUMN "claim_token" uuid NULL, ADD COLUMN "failed_at" timestamptz NULL;
-- Create index "skill_efficacy_evaluations_org_spend_idx" to table: "skill_efficacy_evaluations"
CREATE INDEX CONCURRENTLY "skill_efficacy_evaluations_org_spend_idx" ON "skill_efficacy_evaluations" ("organization_id", "reserved_on") WHERE (reserved_on IS NOT NULL);
-- Create index "skill_efficacy_evaluations_skill_spend_idx" to table: "skill_efficacy_evaluations"
CREATE INDEX CONCURRENTLY "skill_efficacy_evaluations_skill_spend_idx" ON "skill_efficacy_evaluations" ("skill_id", "reserved_on") WHERE (reserved_on IS NOT NULL);
-- Create index "skill_efficacy_evaluations_version_lifetime_spend_idx" to table: "skill_efficacy_evaluations"
CREATE INDEX CONCURRENTLY "skill_efficacy_evaluations_version_lifetime_spend_idx" ON "skill_efficacy_evaluations" ("skill_version_id") WHERE (reserved_on IS NOT NULL);
