-- Modify "remote_sessions" table
ALTER TABLE "remote_sessions" ADD COLUMN "last_used_at" timestamptz NULL;
-- Modify "user_sessions" table
ALTER TABLE "user_sessions" ADD COLUMN "last_used_at" timestamptz NULL;
