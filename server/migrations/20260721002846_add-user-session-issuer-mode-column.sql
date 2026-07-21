-- Modify "user_session_issuers" table
-- NOTE: This column may already exist in production due to schema drift.
-- Using IF NOT EXISTS to make the migration idempotent.
ALTER TABLE "user_session_issuers" ADD COLUMN IF NOT EXISTS "mode" text NULL;
