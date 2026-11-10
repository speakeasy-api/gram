-- atlas:txmode none

-- Create index "trigger_instances_speakeasy_slack_target_uniq" to table: "trigger_instances"
CREATE UNIQUE INDEX CONCURRENTLY "trigger_instances_speakeasy_slack_target_uniq" ON "trigger_instances" ("project_id", "target_ref") WHERE ((definition_slug = 'speakeasy_slack'::text) AND (status = 'active'::text) AND (deleted IS FALSE));
-- Modify "slack_directory_connections" table
ALTER TABLE "slack_directory_connections" ADD COLUMN "default_project_id" uuid NULL, ADD CONSTRAINT "slack_directory_connections_default_project_id_fkey" FOREIGN KEY ("organization_id", "default_project_id") REFERENCES "projects" ("organization_id", "id") ON UPDATE NO ACTION ON DELETE SET NULL ("default_project_id");
-- Create index "slack_directory_connections_default_project_id_key" to table: "slack_directory_connections"
CREATE UNIQUE INDEX CONCURRENTLY "slack_directory_connections_default_project_id_key" ON "slack_directory_connections" ("default_project_id") WHERE (default_project_id IS NOT NULL);
-- Create index "slack_directory_connections_slack_team_id_idx" to table: "slack_directory_connections"
CREATE INDEX CONCURRENTLY "slack_directory_connections_slack_team_id_idx" ON "slack_directory_connections" ("slack_team_id") WHERE ((default_project_id IS NOT NULL) AND (disconnected_at IS NULL));
-- Set comment to column: "default_project_id" on table: "slack_directory_connections"
COMMENT ON COLUMN "slack_directory_connections"."default_project_id" IS 'Project whose managed assistant answers the Speakeasy app in this workspace.
@access: confidential';
