-- Modify "ai_integration_syncs" table
ALTER TABLE "ai_integration_syncs" ADD COLUMN "lease_owner" text NULL, ADD COLUMN "lease_expires_at" timestamptz NULL;
