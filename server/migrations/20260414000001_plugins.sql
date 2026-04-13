-- Create "plugins" table
CREATE TABLE "plugins" (
  "id" uuid NOT NULL DEFAULT generate_uuidv7(),
  "organization_id" text NOT NULL,
  "project_id" uuid NOT NULL,
  "name" text NOT NULL,
  "slug" text NOT NULL,
  "description" text NULL,
  "created_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "updated_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "deleted_at" timestamptz NULL,
  "deleted" boolean NOT NULL GENERATED ALWAYS AS (deleted_at IS NOT NULL) STORED,
  PRIMARY KEY ("id"),
  CONSTRAINT "plugins_organization_id_fkey" FOREIGN KEY ("organization_id") REFERENCES "organization_metadata" ("id") ON UPDATE NO ACTION ON DELETE CASCADE,
  CONSTRAINT "plugins_project_id_fkey" FOREIGN KEY ("project_id") REFERENCES "projects" ("id") ON UPDATE NO ACTION ON DELETE CASCADE
);
-- Create index "plugins_organization_id_slug_key" to table: "plugins"
CREATE UNIQUE INDEX "plugins_organization_id_slug_key" ON "plugins" ("organization_id", "project_id", "slug") WHERE (deleted IS FALSE);
-- Create "plugin_assignments" table
CREATE TABLE "plugin_assignments" (
  "id" uuid NOT NULL DEFAULT generate_uuidv7(),
  "plugin_id" uuid NOT NULL,
  "organization_id" text NOT NULL,
  "principal_urn" text NOT NULL,
  "created_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  PRIMARY KEY ("id"),
  CONSTRAINT "plugin_assignments_organization_id_fkey" FOREIGN KEY ("organization_id") REFERENCES "organization_metadata" ("id") ON UPDATE NO ACTION ON DELETE CASCADE,
  CONSTRAINT "plugin_assignments_plugin_id_fkey" FOREIGN KEY ("plugin_id") REFERENCES "plugins" ("id") ON UPDATE NO ACTION ON DELETE CASCADE
);
-- Create index "plugin_assignments_plugin_id_principal_urn_key" to table: "plugin_assignments"
CREATE UNIQUE INDEX "plugin_assignments_plugin_id_principal_urn_key" ON "plugin_assignments" ("plugin_id", "organization_id", "principal_urn");
-- Create "plugin_servers" table
CREATE TABLE "plugin_servers" (
  "id" uuid NOT NULL DEFAULT generate_uuidv7(),
  "plugin_id" uuid NOT NULL,
  "toolset_id" uuid NULL,
  "registry_id" uuid NULL,
  "registry_server_specifier" text NULL,
  "external_url" text NULL,
  "display_name" text NOT NULL,
  "policy" text NOT NULL DEFAULT 'required',
  "sort_order" integer NOT NULL DEFAULT 0,
  "created_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "updated_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "deleted_at" timestamptz NULL,
  "deleted" boolean NOT NULL GENERATED ALWAYS AS (deleted_at IS NOT NULL) STORED,
  PRIMARY KEY ("id"),
  CONSTRAINT "plugin_servers_plugin_id_fkey" FOREIGN KEY ("plugin_id") REFERENCES "plugins" ("id") ON UPDATE NO ACTION ON DELETE CASCADE,
  CONSTRAINT "plugin_servers_registry_id_fkey" FOREIGN KEY ("registry_id") REFERENCES "mcp_registries" ("id") ON UPDATE NO ACTION ON DELETE SET NULL,
  CONSTRAINT "plugin_servers_toolset_id_fkey" FOREIGN KEY ("toolset_id") REFERENCES "toolsets" ("id") ON UPDATE NO ACTION ON DELETE SET NULL,
  CONSTRAINT "plugin_servers_policy_check" CHECK (policy = ANY (ARRAY['required'::text, 'optional'::text])),
  CONSTRAINT "plugin_servers_source_check" CHECK (((((toolset_id IS NOT NULL))::integer + ((registry_id IS NOT NULL))::integer) + ((external_url IS NOT NULL))::integer) = 1)
);
