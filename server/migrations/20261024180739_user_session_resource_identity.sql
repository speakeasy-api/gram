-- Modify "user_sessions" table
ALTER TABLE "user_sessions" ADD COLUMN "resource_identity" text NULL;
-- Set comment to column: "resource_identity" on table: "user_sessions"
COMMENT ON COLUMN "user_sessions"."resource_identity" IS '@access: confidential';
