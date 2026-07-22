-- Modify "ai_integration_syncs" table
ALTER TABLE "ai_integration_syncs" ALTER COLUMN "schedule" SET NOT NULL, ALTER COLUMN "kind" SET NOT NULL;
