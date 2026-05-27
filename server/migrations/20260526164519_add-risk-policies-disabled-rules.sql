-- Modify "risk_policies" table
ALTER TABLE "risk_policies" ADD COLUMN "disabled_rules" text[] NULL;
