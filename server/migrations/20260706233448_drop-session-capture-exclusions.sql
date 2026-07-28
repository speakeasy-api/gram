-- atlas:txtar

-- checks/destructive.sql --
-- atlas:assert DS102
SELECT NOT EXISTS (SELECT 1 FROM "session_capture_exclusions") AS "is_empty";

-- migration.sql --
-- Drop "session_capture_exclusions" table
DROP TABLE "session_capture_exclusions";
