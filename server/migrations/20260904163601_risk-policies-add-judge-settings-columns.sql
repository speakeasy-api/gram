-- Modify "risk_policies" table
ALTER TABLE "risk_policies" ADD COLUMN "judge_temperature" double precision NULL, ADD COLUMN "judge_fail_open" boolean NULL;
