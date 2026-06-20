-- Modify "risk_custom_detection_rules" table
ALTER TABLE "risk_custom_detection_rules" DROP COLUMN "match_config", ADD COLUMN "detection_cel" text NULL;
-- Modify "risk_policies" table
ALTER TABLE "risk_policies" DROP COLUMN "application_config", ADD COLUMN "scope_include_cel" text NULL, ADD COLUMN "scope_exempt_cel" text NULL;
