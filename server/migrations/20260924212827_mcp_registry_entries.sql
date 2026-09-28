-- Create "mcp_registry_entries" table
CREATE TABLE "mcp_registry_entries" (
  "id" uuid NOT NULL DEFAULT generate_uuidv7(),
  "data" jsonb NOT NULL,
  "published" boolean NOT NULL DEFAULT true,
  "created_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "updated_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  PRIMARY KEY ("id"),
  CONSTRAINT "mcp_registry_entries_name_check" CHECK (COALESCE(((jsonb_typeof((data #> '{server,name}'::text[])) = 'string'::text) AND ((data #>> '{server,name}'::text[]) <> ''::text)), false))
);
-- Create index "mcp_registry_entries_name_key" to table: "mcp_registry_entries"
CREATE UNIQUE INDEX "mcp_registry_entries_name_key" ON "mcp_registry_entries" (((data #>> '{server,name}'::text[])));
