-- atlas:txmode none

-- Modify "risk_policies" table
ALTER TABLE "risk_policies" ADD COLUMN "audience_type" text NOT NULL DEFAULT 'everyone';
ALTER TABLE "risk_policies" ADD CONSTRAINT "risk_policies_audience_type_check" CHECK (audience_type = ANY (ARRAY['everyone'::text, 'targeted'::text])) NOT VALID;
ALTER TABLE "risk_policies" VALIDATE CONSTRAINT "risk_policies_audience_type_check";
-- Create index "risk_policies_project_id_audience_type_idx" to table: "risk_policies"
CREATE INDEX CONCURRENTLY "risk_policies_project_id_audience_type_idx" ON "risk_policies" ("project_id", "audience_type") WHERE (deleted IS FALSE);