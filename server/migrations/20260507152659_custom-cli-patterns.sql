-- Modify "risk_policies" table
ALTER TABLE "risk_policies" ADD COLUMN "custom_cli_patterns" jsonb NOT NULL DEFAULT '[]';
