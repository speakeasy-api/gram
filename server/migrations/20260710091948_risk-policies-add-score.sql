-- Modify "risk_policies" table
ALTER TABLE "risk_policies" ADD COLUMN "score" double precision NOT NULL DEFAULT 5.0;
