-- Modify "risk_results" table
ALTER TABLE "risk_results" DROP COLUMN "start_line", DROP COLUMN "start_column", DROP COLUMN "end_line", DROP COLUMN "end_column", ADD COLUMN "start_pos" integer NULL, ADD COLUMN "end_pos" integer NULL;
