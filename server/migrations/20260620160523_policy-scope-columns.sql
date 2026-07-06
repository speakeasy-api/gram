-- Modify "risk_custom_detection_rules" table
ALTER TABLE "risk_custom_detection_rules" ADD COLUMN "detection_expr" text NULL;
-- Modify "risk_policies" table
ALTER TABLE "risk_policies" ADD COLUMN "scope_include" text NULL, ADD COLUMN "scope_exempt" text NULL;
-- Modify "risk_results" table
ALTER TABLE "risk_results" ADD COLUMN "spans" jsonb NULL;
