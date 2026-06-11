-- Modify "remote_sessions" table
ALTER TABLE "remote_sessions" ALTER COLUMN "access_expires_at" DROP NOT NULL;
