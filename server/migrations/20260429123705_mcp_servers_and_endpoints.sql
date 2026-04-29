-- Create "mcp_servers" table
CREATE TABLE "mcp_servers" (
  "id" uuid NOT NULL DEFAULT generate_uuidv7(),
  "project_id" uuid NOT NULL,
  "environment_id" uuid NULL,
  "external_oauth_server_id" uuid NULL,
  "oauth_proxy_server_id" uuid NULL,
  "remote_mcp_server_id" uuid NULL,
  "toolset_id" uuid NULL,
  "visibility" text NOT NULL,
  "created_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "updated_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "deleted_at" timestamptz NULL,
  "deleted" boolean NOT NULL GENERATED ALWAYS AS (deleted_at IS NOT NULL) STORED,
  PRIMARY KEY ("id"),
  CONSTRAINT "mcp_servers_environment_id_fkey" FOREIGN KEY ("environment_id") REFERENCES "environments" ("id") ON UPDATE NO ACTION ON DELETE SET NULL,
  CONSTRAINT "mcp_servers_external_oauth_server_id_fkey" FOREIGN KEY ("external_oauth_server_id") REFERENCES "external_oauth_server_metadata" ("id") ON UPDATE NO ACTION ON DELETE SET NULL,
  CONSTRAINT "mcp_servers_oauth_proxy_server_id_fkey" FOREIGN KEY ("oauth_proxy_server_id") REFERENCES "oauth_proxy_servers" ("id") ON UPDATE NO ACTION ON DELETE SET NULL,
  CONSTRAINT "mcp_servers_project_id_fkey" FOREIGN KEY ("project_id") REFERENCES "projects" ("id") ON UPDATE NO ACTION ON DELETE CASCADE,
  CONSTRAINT "mcp_servers_remote_mcp_server_id_fkey" FOREIGN KEY ("remote_mcp_server_id") REFERENCES "remote_mcp_servers" ("id") ON UPDATE NO ACTION ON DELETE SET NULL,
  CONSTRAINT "mcp_servers_toolset_id_fkey" FOREIGN KEY ("toolset_id") REFERENCES "toolsets" ("id") ON UPDATE NO ACTION ON DELETE SET NULL,
  CONSTRAINT "mcp_servers_backend_exclusivity_check" CHECK ((remote_mcp_server_id IS NULL) <> (toolset_id IS NULL)),
  CONSTRAINT "mcp_servers_visibility_check" CHECK (visibility <> ''::text)
);
-- Create index "mcp_servers_project_id_idx" to table: "mcp_servers"
CREATE INDEX "mcp_servers_project_id_idx" ON "mcp_servers" ("project_id") WHERE (deleted IS FALSE);
-- Create "mcp_endpoints" table
CREATE TABLE "mcp_endpoints" (
  "id" uuid NOT NULL DEFAULT generate_uuidv7(),
  "project_id" uuid NOT NULL,
  "custom_domain_id" uuid NULL,
  "mcp_server_id" uuid NOT NULL,
  "slug" text NOT NULL,
  "created_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "updated_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "deleted_at" timestamptz NULL,
  "deleted" boolean NOT NULL GENERATED ALWAYS AS (deleted_at IS NOT NULL) STORED,
  PRIMARY KEY ("id"),
  CONSTRAINT "mcp_endpoints_custom_domain_id_fkey" FOREIGN KEY ("custom_domain_id") REFERENCES "custom_domains" ("id") ON UPDATE NO ACTION ON DELETE SET NULL,
  CONSTRAINT "mcp_endpoints_mcp_server_id_fkey" FOREIGN KEY ("mcp_server_id") REFERENCES "mcp_servers" ("id") ON UPDATE NO ACTION ON DELETE CASCADE,
  CONSTRAINT "mcp_endpoints_project_id_fkey" FOREIGN KEY ("project_id") REFERENCES "projects" ("id") ON UPDATE NO ACTION ON DELETE CASCADE,
  CONSTRAINT "mcp_endpoints_slug_check" CHECK ((slug <> ''::text) AND (char_length(slug) <= 128))
);
-- Create index "mcp_endpoints_custom_domain_id_idx" to table: "mcp_endpoints"
CREATE INDEX "mcp_endpoints_custom_domain_id_idx" ON "mcp_endpoints" ("custom_domain_id") WHERE (deleted IS FALSE);
-- Create index "mcp_endpoints_custom_domain_id_slug_key" to table: "mcp_endpoints"
CREATE UNIQUE INDEX "mcp_endpoints_custom_domain_id_slug_key" ON "mcp_endpoints" ("custom_domain_id", "slug") WHERE ((custom_domain_id IS NOT NULL) AND (deleted IS FALSE));
-- Create index "mcp_endpoints_mcp_server_id_idx" to table: "mcp_endpoints"
CREATE INDEX "mcp_endpoints_mcp_server_id_idx" ON "mcp_endpoints" ("mcp_server_id") WHERE (deleted IS FALSE);
-- Create index "mcp_endpoints_project_id_idx" to table: "mcp_endpoints"
CREATE INDEX "mcp_endpoints_project_id_idx" ON "mcp_endpoints" ("project_id") WHERE (deleted IS FALSE);
-- Create index "mcp_endpoints_slug_null_custom_domain_id_key" to table: "mcp_endpoints"
CREATE UNIQUE INDEX "mcp_endpoints_slug_null_custom_domain_id_key" ON "mcp_endpoints" ("slug") WHERE ((custom_domain_id IS NULL) AND (deleted IS FALSE));
