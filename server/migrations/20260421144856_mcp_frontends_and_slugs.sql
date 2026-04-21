-- Create "mcp_frontends" table
CREATE TABLE "mcp_frontends" (
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
  CONSTRAINT "mcp_frontends_environment_id_fkey" FOREIGN KEY ("environment_id") REFERENCES "environments" ("id") ON UPDATE NO ACTION ON DELETE SET NULL,
  CONSTRAINT "mcp_frontends_external_oauth_server_id_fkey" FOREIGN KEY ("external_oauth_server_id") REFERENCES "external_oauth_server_metadata" ("id") ON UPDATE NO ACTION ON DELETE SET NULL,
  CONSTRAINT "mcp_frontends_oauth_proxy_server_id_fkey" FOREIGN KEY ("oauth_proxy_server_id") REFERENCES "oauth_proxy_servers" ("id") ON UPDATE NO ACTION ON DELETE SET NULL,
  CONSTRAINT "mcp_frontends_project_id_fkey" FOREIGN KEY ("project_id") REFERENCES "projects" ("id") ON UPDATE NO ACTION ON DELETE CASCADE,
  CONSTRAINT "mcp_frontends_remote_mcp_server_id_fkey" FOREIGN KEY ("remote_mcp_server_id") REFERENCES "remote_mcp_servers" ("id") ON UPDATE NO ACTION ON DELETE SET NULL,
  CONSTRAINT "mcp_frontends_toolset_id_fkey" FOREIGN KEY ("toolset_id") REFERENCES "toolsets" ("id") ON UPDATE NO ACTION ON DELETE SET NULL,
  CONSTRAINT "mcp_frontends_backend_exclusivity_check" CHECK ((remote_mcp_server_id IS NULL) <> (toolset_id IS NULL)),
  CONSTRAINT "mcp_frontends_visibility_check" CHECK (visibility <> ''::text)
);
-- Create index "mcp_frontends_environment_id_idx" to table: "mcp_frontends"
CREATE INDEX "mcp_frontends_environment_id_idx" ON "mcp_frontends" ("environment_id") WHERE (deleted IS FALSE);
-- Create index "mcp_frontends_external_oauth_server_id_idx" to table: "mcp_frontends"
CREATE INDEX "mcp_frontends_external_oauth_server_id_idx" ON "mcp_frontends" ("external_oauth_server_id") WHERE (deleted IS FALSE);
-- Create index "mcp_frontends_oauth_proxy_server_id_idx" to table: "mcp_frontends"
CREATE INDEX "mcp_frontends_oauth_proxy_server_id_idx" ON "mcp_frontends" ("oauth_proxy_server_id") WHERE (deleted IS FALSE);
-- Create index "mcp_frontends_project_id_idx" to table: "mcp_frontends"
CREATE INDEX "mcp_frontends_project_id_idx" ON "mcp_frontends" ("project_id") WHERE (deleted IS FALSE);
-- Create index "mcp_frontends_remote_mcp_server_id_idx" to table: "mcp_frontends"
CREATE INDEX "mcp_frontends_remote_mcp_server_id_idx" ON "mcp_frontends" ("remote_mcp_server_id") WHERE (deleted IS FALSE);
-- Create index "mcp_frontends_toolset_id_idx" to table: "mcp_frontends"
CREATE INDEX "mcp_frontends_toolset_id_idx" ON "mcp_frontends" ("toolset_id") WHERE (deleted IS FALSE);
-- Create "mcp_slugs" table
CREATE TABLE "mcp_slugs" (
  "id" uuid NOT NULL DEFAULT generate_uuidv7(),
  "project_id" uuid NOT NULL,
  "custom_domain_id" uuid NULL,
  "mcp_frontend_id" uuid NOT NULL,
  "slug" text NOT NULL,
  "created_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "updated_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "deleted_at" timestamptz NULL,
  "deleted" boolean NOT NULL GENERATED ALWAYS AS (deleted_at IS NOT NULL) STORED,
  PRIMARY KEY ("id"),
  CONSTRAINT "mcp_slugs_custom_domain_id_fkey" FOREIGN KEY ("custom_domain_id") REFERENCES "custom_domains" ("id") ON UPDATE NO ACTION ON DELETE SET NULL,
  CONSTRAINT "mcp_slugs_mcp_frontend_id_fkey" FOREIGN KEY ("mcp_frontend_id") REFERENCES "mcp_frontends" ("id") ON UPDATE NO ACTION ON DELETE CASCADE,
  CONSTRAINT "mcp_slugs_project_id_fkey" FOREIGN KEY ("project_id") REFERENCES "projects" ("id") ON UPDATE NO ACTION ON DELETE CASCADE,
  CONSTRAINT "mcp_slugs_slug_check" CHECK ((slug <> ''::text) AND (char_length(slug) <= 128))
);
-- Create index "mcp_slugs_custom_domain_id_idx" to table: "mcp_slugs"
CREATE INDEX "mcp_slugs_custom_domain_id_idx" ON "mcp_slugs" ("custom_domain_id") WHERE (deleted IS FALSE);
-- Create index "mcp_slugs_custom_domain_id_slug_key" to table: "mcp_slugs"
CREATE UNIQUE INDEX "mcp_slugs_custom_domain_id_slug_key" ON "mcp_slugs" ("custom_domain_id", "slug") WHERE ((custom_domain_id IS NOT NULL) AND (deleted IS FALSE));
-- Create index "mcp_slugs_mcp_frontend_id_idx" to table: "mcp_slugs"
CREATE INDEX "mcp_slugs_mcp_frontend_id_idx" ON "mcp_slugs" ("mcp_frontend_id") WHERE (deleted IS FALSE);
-- Create index "mcp_slugs_project_id_idx" to table: "mcp_slugs"
CREATE INDEX "mcp_slugs_project_id_idx" ON "mcp_slugs" ("project_id") WHERE (deleted IS FALSE);
-- Create index "mcp_slugs_slug_null_custom_domain_id_key" to table: "mcp_slugs"
CREATE UNIQUE INDEX "mcp_slugs_slug_null_custom_domain_id_key" ON "mcp_slugs" ("slug") WHERE ((custom_domain_id IS NULL) AND (deleted IS FALSE));
