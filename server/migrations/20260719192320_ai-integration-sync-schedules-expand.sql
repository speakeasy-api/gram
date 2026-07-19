-- atlas:txmode none

-- Drop index "ai_integration_syncs_config_id_schedule_key" from table: "ai_integration_syncs"
DROP INDEX CONCURRENTLY "ai_integration_syncs_config_id_schedule_key";
-- Create index "ai_integration_syncs_config_id_schedule_key" to table: "ai_integration_syncs"
CREATE UNIQUE INDEX CONCURRENTLY "ai_integration_syncs_config_id_schedule_key" ON "ai_integration_syncs" ("ai_integration_config_id", "schedule");
