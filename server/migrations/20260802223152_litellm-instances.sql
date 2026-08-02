-- atlas:txmode none

-- Create index "api_keys_organization_id_project_id_id_key" to table: "api_keys"
CREATE UNIQUE INDEX CONCURRENTLY "api_keys_organization_id_project_id_id_key" ON "api_keys" ("organization_id", "project_id", "id");
-- Create index "projects_organization_id_id_key" to table: "projects"
CREATE UNIQUE INDEX CONCURRENTLY "projects_organization_id_id_key" ON "projects" ("organization_id", "id");
-- Create "litellm_instances" table
CREATE TABLE "litellm_instances" (
  "id" uuid NOT NULL DEFAULT generate_uuidv7(),
  "organization_id" text NOT NULL,
  "project_id" uuid NOT NULL,
  "api_key_id" uuid NOT NULL,
  "created_by_user_id" text NOT NULL,
  "name" text NOT NULL,
  "failure_posture" text NOT NULL DEFAULT 'fail_closed',
  "created_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "updated_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "deleted_at" timestamptz NULL,
  "deleted" boolean NOT NULL GENERATED ALWAYS AS (deleted_at IS NOT NULL) STORED,
  PRIMARY KEY ("id"),
  CONSTRAINT "litellm_instances_api_key_tenant_fkey" FOREIGN KEY ("organization_id", "project_id", "api_key_id") REFERENCES "api_keys" ("organization_id", "project_id", "id") ON UPDATE NO ACTION ON DELETE RESTRICT,
  CONSTRAINT "litellm_instances_organization_id_fkey" FOREIGN KEY ("organization_id") REFERENCES "organization_metadata" ("id") ON UPDATE NO ACTION ON DELETE CASCADE,
  CONSTRAINT "litellm_instances_project_tenant_fkey" FOREIGN KEY ("organization_id", "project_id") REFERENCES "projects" ("organization_id", "id") ON UPDATE NO ACTION ON DELETE CASCADE,
  CONSTRAINT "litellm_instances_name_check" CHECK ((name <> ''::text) AND (char_length(name) <= 255))
);
-- Create index "litellm_instances_api_key_tenant_key" to table: "litellm_instances"
CREATE UNIQUE INDEX "litellm_instances_api_key_tenant_key" ON "litellm_instances" ("organization_id", "project_id", "api_key_id");
-- Create index "litellm_instances_project_id_name_key" to table: "litellm_instances"
CREATE UNIQUE INDEX "litellm_instances_project_id_name_key" ON "litellm_instances" ("project_id", "name") WHERE (deleted IS FALSE);
