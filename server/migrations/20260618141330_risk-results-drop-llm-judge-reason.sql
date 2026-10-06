-- atlas:txtar

-- checks/destructive.sql --
-- atlas:assert DS103
SELECT NOT EXISTS (SELECT 1 FROM "risk_results" WHERE "llm_judge_reason" IS NOT NULL) AS "is_empty";

-- migration.sql --
-- Modify "risk_results" table
ALTER TABLE "risk_results" DROP COLUMN "llm_judge_reason";
