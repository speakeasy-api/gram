-- atlas:txmode none

-- Modify "mcp_registry_entries" table
ALTER TABLE "mcp_registry_entries" ADD COLUMN "server_id" uuid NULL;
-- Create index "mcp_registry_entries_server_id_id_key" to table: "mcp_registry_entries"
CREATE UNIQUE INDEX CONCURRENTLY "mcp_registry_entries_server_id_id_key" ON "mcp_registry_entries" ("server_id", "id");
-- Set comment to column: "server_id" on table: "mcp_registry_entries"
COMMENT ON COLUMN "mcp_registry_entries"."server_id" IS '@access: confidential';
-- Create "mcp_registry_servers" table
CREATE TABLE "mcp_registry_servers" (
  "id" uuid NOT NULL DEFAULT generate_uuidv7(),
  "name" text NOT NULL,
  "visible" boolean NOT NULL DEFAULT true,
  "default_entry_id" uuid NULL,
  "created_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "updated_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  PRIMARY KEY ("id")
);
-- Create index "mcp_registry_servers_name_key" to table: "mcp_registry_servers"
CREATE UNIQUE INDEX "mcp_registry_servers_name_key" ON "mcp_registry_servers" ("name");
-- Set comment to column: "id" on table: "mcp_registry_servers"
COMMENT ON COLUMN "mcp_registry_servers"."id" IS '@access: confidential';
-- Set comment to column: "name" on table: "mcp_registry_servers"
COMMENT ON COLUMN "mcp_registry_servers"."name" IS '@access: confidential';
-- Set comment to column: "visible" on table: "mcp_registry_servers"
COMMENT ON COLUMN "mcp_registry_servers"."visible" IS '@access: confidential';
-- Set comment to column: "default_entry_id" on table: "mcp_registry_servers"
COMMENT ON COLUMN "mcp_registry_servers"."default_entry_id" IS '@access: confidential';
-- Set comment to column: "created_at" on table: "mcp_registry_servers"
COMMENT ON COLUMN "mcp_registry_servers"."created_at" IS '@access: confidential';
-- Set comment to column: "updated_at" on table: "mcp_registry_servers"
COMMENT ON COLUMN "mcp_registry_servers"."updated_at" IS '@access: confidential';
-- Modify "mcp_registry_entries" table
ALTER TABLE "mcp_registry_entries" ADD CONSTRAINT "mcp_registry_entries_server_id_fkey" FOREIGN KEY ("server_id") REFERENCES "mcp_registry_servers" ("id") ON UPDATE NO ACTION ON DELETE SET NULL;
-- Modify "mcp_registry_servers" table
ALTER TABLE "mcp_registry_servers" ADD CONSTRAINT "mcp_registry_servers_default_entry_fkey" FOREIGN KEY ("id", "default_entry_id") REFERENCES "mcp_registry_entries" ("server_id", "id") ON UPDATE NO ACTION ON DELETE NO ACTION DEFERRABLE INITIALLY DEFERRED;
