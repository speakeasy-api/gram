-- Modify "toolset_versions" table
ALTER TABLE "toolset_versions" ADD COLUMN "resource_urns" text[] NOT NULL DEFAULT ARRAY[]::text[];
-- Create "function_resource_definitions" table
CREATE TABLE "function_resource_definitions" (
  "id" uuid NOT NULL DEFAULT generate_uuidv7(),
  "resource_urn" text NOT NULL,
  "project_id" uuid NOT NULL,
  "deployment_id" uuid NOT NULL,
  "function_id" uuid NOT NULL,
  "runtime" text NOT NULL,
  "name" text NOT NULL,
  "description" text NOT NULL,
  "uri" text NOT NULL,
  "title" text NULL,
  "mime_type" text NULL,
  "variables" jsonb NULL,
  "created_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "updated_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "deleted_at" timestamptz NULL,
  "deleted" boolean NOT NULL GENERATED ALWAYS AS (deleted_at IS NOT NULL) STORED,
  PRIMARY KEY ("id"),
  CONSTRAINT "function_resource_definitions_deployment_id_fkey" FOREIGN KEY ("deployment_id") REFERENCES "deployments" ("id") ON UPDATE NO ACTION ON DELETE CASCADE,
  CONSTRAINT "function_resource_definitions_function_id_fkey" FOREIGN KEY ("function_id") REFERENCES "deployments_functions" ("id") ON UPDATE NO ACTION ON DELETE CASCADE,
  CONSTRAINT "function_resource_definitions_project_id_fkey" FOREIGN KEY ("project_id") REFERENCES "projects" ("id") ON UPDATE NO ACTION ON DELETE CASCADE
);
