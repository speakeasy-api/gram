-- Modify "user_sessions" table
ALTER TABLE "user_sessions" ALTER COLUMN "refresh_token_hash" DROP NOT NULL;
