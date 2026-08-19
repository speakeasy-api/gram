-- Create "meta_mcp_servers" table
CREATE TABLE "meta_mcp_servers" (
  "id" uuid NOT NULL DEFAULT generate_uuidv7(),
  "organization_id" text NOT NULL,
  "project_id" uuid NOT NULL,
  "plugin_id" uuid NOT NULL,
  "user_session_issuer_id" uuid NULL,
  "name" text NOT NULL,
  "slug" text NOT NULL,
  "created_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "updated_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "deleted_at" timestamptz NULL,
  "deleted" boolean NOT NULL GENERATED ALWAYS AS (deleted_at IS NOT NULL) STORED,
  PRIMARY KEY ("id"),
  CONSTRAINT "meta_mcp_servers_organization_id_project_id_fkey" FOREIGN KEY ("organization_id", "project_id") REFERENCES "projects" ("organization_id", "id") ON UPDATE NO ACTION ON DELETE CASCADE,
  CONSTRAINT "meta_mcp_servers_project_id_plugin_id_fkey" FOREIGN KEY ("project_id", "plugin_id") REFERENCES "plugins" ("project_id", "id") ON UPDATE NO ACTION ON DELETE RESTRICT,
  CONSTRAINT "meta_mcp_servers_project_id_user_session_issuer_id_fkey" FOREIGN KEY ("project_id", "user_session_issuer_id") REFERENCES "user_session_issuers" ("project_id", "id") ON UPDATE NO ACTION ON DELETE RESTRICT,
  CONSTRAINT "meta_mcp_servers_name_check" CHECK ((name <> ''::text) AND (char_length(name) <= 100)),
  CONSTRAINT "meta_mcp_servers_slug_check" CHECK ((slug <> ''::text) AND (char_length(slug) <= 100))
);
-- Create index "meta_mcp_servers_organization_id_project_id_slug_key" to table: "meta_mcp_servers"
CREATE UNIQUE INDEX "meta_mcp_servers_organization_id_project_id_slug_key" ON "meta_mcp_servers" ("organization_id", "project_id", "slug") WHERE (deleted IS FALSE);
-- Create index "meta_mcp_servers_project_id_idx" to table: "meta_mcp_servers"
CREATE INDEX "meta_mcp_servers_project_id_idx" ON "meta_mcp_servers" ("project_id") WHERE (deleted IS FALSE);
-- Create index "meta_mcp_servers_project_id_plugin_id_key" to table: "meta_mcp_servers"
CREATE UNIQUE INDEX "meta_mcp_servers_project_id_plugin_id_key" ON "meta_mcp_servers" ("project_id", "plugin_id") WHERE (deleted IS FALSE);
