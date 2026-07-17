-- atlas:txmode none

-- Modify "ai_integration_syncs" table
ALTER TABLE "ai_integration_syncs" ADD COLUMN "schedule" text NULL, ADD COLUMN "kind" text NULL;
-- Create index "ai_integration_syncs_config_id_schedule_key" to table: "ai_integration_syncs"
CREATE UNIQUE INDEX CONCURRENTLY "ai_integration_syncs_config_id_schedule_key" ON "ai_integration_syncs" ("ai_integration_config_id", "schedule");
