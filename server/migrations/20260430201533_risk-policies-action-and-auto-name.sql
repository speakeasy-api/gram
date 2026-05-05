-- Modify "risk_policies" table
ALTER TABLE "risk_policies" ADD COLUMN "action" text NOT NULL DEFAULT 'flag', ADD COLUMN "auto_name" boolean NOT NULL DEFAULT true;
