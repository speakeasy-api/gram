-- Modify "risk_policies" table
ALTER TABLE "risk_policies" ADD COLUMN "prompt_injection_rules" text[] NULL;
