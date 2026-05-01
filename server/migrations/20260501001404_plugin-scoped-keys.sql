-- atlas:txmode none

-- Modify "api_keys" table
ALTER TABLE "api_keys" ADD COLUMN "toolset_id" uuid NULL, ADD COLUMN "plugin_id" uuid NULL, ADD COLUMN "system_managed" boolean NOT NULL DEFAULT false;
-- Create index "api_keys_plugin_id_idx" to table: "api_keys"
CREATE INDEX CONCURRENTLY "api_keys_plugin_id_idx" ON "api_keys" ("plugin_id") WHERE ((plugin_id IS NOT NULL) AND (deleted IS FALSE));
-- Create index "api_keys_toolset_id_idx" to table: "api_keys"
CREATE INDEX CONCURRENTLY "api_keys_toolset_id_idx" ON "api_keys" ("toolset_id") WHERE ((toolset_id IS NOT NULL) AND (deleted IS FALSE));
-- Modify "plugin_servers" table
ALTER TABLE "plugin_servers" ADD COLUMN "api_key_id" uuid NULL, ADD CONSTRAINT "plugin_servers_api_key_id_fkey" FOREIGN KEY ("api_key_id") REFERENCES "api_keys" ("id") ON UPDATE NO ACTION ON DELETE SET NULL;
