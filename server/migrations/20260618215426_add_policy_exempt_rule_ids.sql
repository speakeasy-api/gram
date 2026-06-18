-- Modify "risk_policies" table
ALTER TABLE "risk_policies" ADD COLUMN "exempt_rule_ids" text[] NOT NULL DEFAULT '{}';
