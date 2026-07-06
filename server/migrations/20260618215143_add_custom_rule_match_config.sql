-- Modify "risk_custom_detection_rules" table
ALTER TABLE "risk_custom_detection_rules" ADD COLUMN "match_config" jsonb NULL;
