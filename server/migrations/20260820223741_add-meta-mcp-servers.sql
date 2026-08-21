-- atlas:txmode none

-- Create "meta_mcp_servers" table
CREATE TABLE "meta_mcp_servers" (
  "id" uuid NOT NULL DEFAULT generate_uuidv7(),
  "organization_id" text NOT NULL,
  "project_id" uuid NOT NULL,
  "user_session_issuer_id" uuid NULL,
  "name" text NOT NULL,
  "created_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "updated_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "deleted_at" timestamptz NULL,
  "deleted" boolean NOT NULL GENERATED ALWAYS AS (deleted_at IS NOT NULL) STORED,
  PRIMARY KEY ("id"),
  CONSTRAINT "meta_mcp_servers_organization_id_project_id_fkey" FOREIGN KEY ("organization_id", "project_id") REFERENCES "projects" ("organization_id", "id") ON UPDATE NO ACTION ON DELETE CASCADE,
  CONSTRAINT "meta_mcp_servers_project_id_user_session_issuer_id_fkey" FOREIGN KEY ("project_id", "user_session_issuer_id") REFERENCES "user_session_issuers" ("project_id", "id") ON UPDATE NO ACTION ON DELETE RESTRICT,
  CONSTRAINT "meta_mcp_servers_name_check" CHECK ((name <> ''::text) AND (char_length(name) <= 100))
);
-- Create index "meta_mcp_servers_project_id_id_key" to table: "meta_mcp_servers"
CREATE UNIQUE INDEX "meta_mcp_servers_project_id_id_key" ON "meta_mcp_servers" ("project_id", "id");
-- Create index "meta_mcp_servers_project_id_idx" to table: "meta_mcp_servers"
CREATE INDEX "meta_mcp_servers_project_id_idx" ON "meta_mcp_servers" ("project_id") WHERE (deleted IS FALSE);
-- Modify "mcp_endpoints" table
ALTER TABLE "mcp_endpoints" ADD CONSTRAINT "mcp_endpoints_backend_exclusivity_check" CHECK (num_nonnulls(mcp_server_id, meta_mcp_server_id) = 1) NOT VALID, ALTER COLUMN "mcp_server_id" DROP NOT NULL, ADD COLUMN "meta_mcp_server_id" uuid NULL, ADD CONSTRAINT "mcp_endpoints_project_id_meta_mcp_server_id_fkey" FOREIGN KEY ("project_id", "meta_mcp_server_id") REFERENCES "meta_mcp_servers" ("project_id", "id") ON UPDATE NO ACTION ON DELETE CASCADE NOT VALID;
ALTER TABLE "mcp_endpoints" VALIDATE CONSTRAINT "mcp_endpoints_backend_exclusivity_check";
ALTER TABLE "mcp_endpoints" VALIDATE CONSTRAINT "mcp_endpoints_project_id_meta_mcp_server_id_fkey";
-- Create index "mcp_endpoints_meta_mcp_server_id_idx" to table: "mcp_endpoints"
CREATE INDEX CONCURRENTLY "mcp_endpoints_meta_mcp_server_id_idx" ON "mcp_endpoints" ("meta_mcp_server_id") WHERE (deleted IS FALSE);
-- Create "meta_mcp_server_members" table
CREATE TABLE "meta_mcp_server_members" (
  "id" uuid NOT NULL DEFAULT generate_uuidv7(),
  "project_id" uuid NOT NULL,
  "meta_mcp_server_id" uuid NOT NULL,
  "mcp_server_id" uuid NOT NULL,
  "sort_order" integer NOT NULL DEFAULT 0,
  "created_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "updated_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "deleted_at" timestamptz NULL,
  "deleted" boolean NOT NULL GENERATED ALWAYS AS (deleted_at IS NOT NULL) STORED,
  PRIMARY KEY ("id"),
  CONSTRAINT "meta_mcp_server_members_project_id_fkey" FOREIGN KEY ("project_id") REFERENCES "projects" ("id") ON UPDATE NO ACTION ON DELETE CASCADE,
  CONSTRAINT "meta_mcp_server_members_project_id_mcp_server_id_fkey" FOREIGN KEY ("project_id", "mcp_server_id") REFERENCES "mcp_servers" ("project_id", "id") ON UPDATE NO ACTION ON DELETE CASCADE,
  CONSTRAINT "meta_mcp_server_members_project_id_meta_mcp_server_id_fkey" FOREIGN KEY ("project_id", "meta_mcp_server_id") REFERENCES "meta_mcp_servers" ("project_id", "id") ON UPDATE NO ACTION ON DELETE CASCADE
);
-- Create index "meta_mcp_server_members_mcp_server_id_idx" to table: "meta_mcp_server_members"
CREATE INDEX "meta_mcp_server_members_mcp_server_id_idx" ON "meta_mcp_server_members" ("mcp_server_id") WHERE (deleted IS FALSE);
-- Create index "meta_mcp_server_members_meta_mcp_server_id_idx" to table: "meta_mcp_server_members"
CREATE INDEX "meta_mcp_server_members_meta_mcp_server_id_idx" ON "meta_mcp_server_members" ("meta_mcp_server_id", "sort_order", "created_at", "id") WHERE (deleted IS FALSE);
-- Create index "meta_mcp_server_members_meta_mcp_server_id_mcp_server_id_key" to table: "meta_mcp_server_members"
CREATE UNIQUE INDEX "meta_mcp_server_members_meta_mcp_server_id_mcp_server_id_key" ON "meta_mcp_server_members" ("meta_mcp_server_id", "mcp_server_id") WHERE (deleted IS FALSE);
