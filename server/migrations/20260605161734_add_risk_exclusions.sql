-- atlas:txmode none

-- Drop index "risk_results_project_found_idx" from table: "risk_results"
DROP INDEX CONCURRENTLY "risk_results_project_found_idx";
-- Modify "risk_results" table
ALTER TABLE "risk_results" ADD COLUMN "excluded_at" timestamptz NULL, ADD COLUMN "excluded_exclusion_id" uuid NULL;
-- Create index "risk_results_project_found_idx" to table: "risk_results"
CREATE INDEX CONCURRENTLY "risk_results_project_found_idx" ON "risk_results" ("project_id", "created_at" DESC) WHERE ((found IS TRUE) AND (excluded_at IS NULL));
-- Create index "risk_results_excluded_exclusion_idx" to table: "risk_results"
CREATE INDEX CONCURRENTLY "risk_results_excluded_exclusion_idx" ON "risk_results" ("excluded_exclusion_id") WHERE (excluded_exclusion_id IS NOT NULL);
-- Create index "risk_results_policy_match_idx" to table: "risk_results"
CREATE INDEX CONCURRENTLY "risk_results_policy_match_idx" ON "risk_results" ("project_id", "risk_policy_id", "rule_id");
-- Create "risk_exclusions" table
CREATE TABLE "risk_exclusions" (
  "id" uuid NOT NULL DEFAULT generate_uuidv7(),
  "project_id" uuid NOT NULL,
  "organization_id" text NOT NULL,
  "risk_policy_id" uuid NULL,
  "match_type" text NOT NULL,
  "match_value" text NOT NULL,
  "rule_id_filter" text NULL,
  "source_filter" text NULL,
  "enabled" boolean NOT NULL DEFAULT true,
  "created_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "updated_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "deleted_at" timestamptz NULL,
  "deleted" boolean NOT NULL GENERATED ALWAYS AS (deleted_at IS NOT NULL) STORED,
  PRIMARY KEY ("id"),
  CONSTRAINT "risk_exclusions_organization_id_fkey" FOREIGN KEY ("organization_id") REFERENCES "organization_metadata" ("id") ON UPDATE NO ACTION ON DELETE CASCADE,
  CONSTRAINT "risk_exclusions_project_id_fkey" FOREIGN KEY ("project_id") REFERENCES "projects" ("id") ON UPDATE NO ACTION ON DELETE CASCADE,
  CONSTRAINT "risk_exclusions_risk_policy_id_fkey" FOREIGN KEY ("risk_policy_id") REFERENCES "risk_policies" ("id") ON UPDATE NO ACTION ON DELETE CASCADE,
  CONSTRAINT "risk_exclusions_match_type_check" CHECK (match_type = ANY (ARRAY['exact'::text, 'regex'::text, 'rule_id'::text, 'source'::text, 'entity_type'::text]))
);
-- Create index "risk_exclusions_project_policy_idx" to table: "risk_exclusions"
CREATE INDEX "risk_exclusions_project_policy_idx" ON "risk_exclusions" ("project_id", "risk_policy_id") WHERE (deleted IS FALSE);
