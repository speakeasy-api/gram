-- atlas:txmode none

-- Modify "risk_policies" table
ALTER TABLE "risk_policies" ADD COLUMN "idempotency_key" text NULL;
-- Create index "risk_policies_project_id_idempotency_key_idx" to table: "risk_policies"
CREATE UNIQUE INDEX CONCURRENTLY "risk_policies_project_id_idempotency_key_idx" ON "risk_policies" ("project_id", "idempotency_key") WHERE ((idempotency_key IS NOT NULL) AND (deleted IS FALSE));
