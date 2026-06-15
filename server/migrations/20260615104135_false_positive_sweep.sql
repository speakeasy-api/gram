-- atlas:txmode none

-- Drop index "risk_results_project_found_idx" from table: "risk_results"
DROP INDEX CONCURRENTLY "risk_results_project_found_idx";
-- Modify "risk_results" table
ALTER TABLE "risk_results" ADD COLUMN "false_positive_at" timestamptz NULL, ADD COLUMN "false_positive_reason" text NULL;
-- Create index "risk_results_project_found_idx" to table: "risk_results"
CREATE INDEX CONCURRENTLY "risk_results_project_found_idx" ON "risk_results" ("project_id", "created_at" DESC) WHERE ((found IS TRUE) AND (excluded_at IS NULL) AND (false_positive_at IS NULL));
