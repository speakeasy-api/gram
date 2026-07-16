-- atlas:txmode none

-- Create index "plugins_project_id_id_key" to table: "plugins"
CREATE UNIQUE INDEX CONCURRENTLY "plugins_project_id_id_key" ON "plugins" ("project_id", "id");
-- Drop index "skill_distributions_project_id_skill_id_channel_key" from table: "skill_distributions"
DROP INDEX CONCURRENTLY "skill_distributions_project_id_skill_id_channel_key";
-- Modify "skill_distributions" table
ALTER TABLE "skill_distributions" ADD CONSTRAINT "skill_distributions_plugin_id_audience_check" CHECK ((plugin_id IS NULL) OR (audience IS NULL)), ADD COLUMN "plugin_id" uuid NULL, ADD CONSTRAINT "skill_distributions_project_id_plugin_id_fkey" FOREIGN KEY ("project_id", "plugin_id") REFERENCES "plugins" ("project_id", "id") ON UPDATE NO ACTION ON DELETE NO ACTION;
-- Create index "skill_distributions_plugin_id_idx" to table: "skill_distributions"
CREATE INDEX CONCURRENTLY "skill_distributions_plugin_id_idx" ON "skill_distributions" ("plugin_id");
-- Create index "skill_distributions_project_id_skill_id_channel_plugin_id_key" to table: "skill_distributions"
CREATE UNIQUE INDEX CONCURRENTLY "skill_distributions_project_id_skill_id_channel_plugin_id_key" ON "skill_distributions" ("project_id", "skill_id", "channel", "plugin_id") NULLS NOT DISTINCT WHERE (revoked_at IS NULL);
