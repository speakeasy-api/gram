-- Modify "risk_custom_detection_rules" table
ALTER TABLE "risk_custom_detection_rules" ADD COLUMN "match_config" jsonb NULL;
-- Modify "risk_policies" table
ALTER TABLE "risk_policies" ADD COLUMN "exempt_rule_ids" text[] NOT NULL DEFAULT '{}', ADD COLUMN "application_config" jsonb NULL;
