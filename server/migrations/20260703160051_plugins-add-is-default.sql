-- atlas:txmode none

-- Modify "plugins" table
ALTER TABLE "plugins" ADD COLUMN "is_default" boolean NULL DEFAULT false;
-- Create index "plugins_project_id_idx" to table: "plugins"
CREATE INDEX CONCURRENTLY "plugins_project_id_idx" ON "plugins" ("project_id");
-- Create index "plugins_project_id_is_default_key" to table: "plugins"
CREATE UNIQUE INDEX CONCURRENTLY "plugins_project_id_is_default_key" ON "plugins" ("project_id") WHERE ((is_default IS TRUE) AND (deleted IS FALSE));
-- Set comment to column: "is_default" on table: "plugins"
COMMENT ON COLUMN "plugins"."is_default" IS 'Marks the fallback plugin new servers land in when not explicitly routed to a named plugin. At most one true per project (see plugins_project_id_is_default_key).';
