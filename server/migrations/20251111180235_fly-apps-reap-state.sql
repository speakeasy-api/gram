-- atlas:txmode none

-- Drop index "fly_apps_project_deployment_function_key" from table: "fly_apps"
DROP INDEX CONCURRENTLY "fly_apps_project_deployment_function_key";
-- Modify "fly_apps" table
ALTER TABLE "fly_apps" ADD COLUMN "reap_error" text NULL;
-- Create index "fly_apps_project_deployment_function_active_key" to table: "fly_apps"
CREATE UNIQUE INDEX CONCURRENTLY "fly_apps_project_deployment_function_active_key" ON "fly_apps" ("project_id", "deployment_id", "function_id") WHERE (reaped_at IS NULL);
-- Create index "fly_apps_reaper_idx" to table: "fly_apps"
CREATE INDEX CONCURRENTLY "fly_apps_reaper_idx" ON "fly_apps" ("project_id", "created_at" DESC) WHERE ((status = 'ready'::text) AND (reaped_at IS NULL));
