-- atlas:txmode none

-- Create index "skill_sync_receipts_project_id_user_id_hostname_provider_skill_" to table: "skill_sync_receipts"
CREATE INDEX CONCURRENTLY "skill_sync_receipts_project_id_user_id_hostname_provider_skill_" ON "skill_sync_receipts" ("project_id", "user_id", "hostname", "provider", "skill_id");
