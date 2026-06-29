-- Modify "risk_policies" table
ALTER TABLE "risk_policies" ADD COLUMN "analyzer_config" jsonb NOT NULL DEFAULT '{}';
