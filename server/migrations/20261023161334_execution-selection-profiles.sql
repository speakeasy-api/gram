-- Create "execution_selection_profiles" table
CREATE TABLE "execution_selection_profiles" (
  "id" uuid NOT NULL DEFAULT generate_uuidv7(),
  "organization_id" text NOT NULL,
  "project_id" uuid NOT NULL,
  "assistant_id" uuid NOT NULL,
  "trigger_instance_id" uuid NOT NULL,
  "execution_context" text NOT NULL,
  "created_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "updated_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "deleted_at" timestamptz NULL,
  "deleted" boolean NOT NULL GENERATED ALWAYS AS (deleted_at IS NOT NULL) STORED,
  PRIMARY KEY ("id"),
  CONSTRAINT "execution_selection_profiles_assistant_fkey" FOREIGN KEY ("organization_id", "project_id", "assistant_id") REFERENCES "assistants" ("organization_id", "project_id", "id") ON UPDATE NO ACTION ON DELETE CASCADE,
  CONSTRAINT "execution_selection_profiles_project_tenant_fkey" FOREIGN KEY ("organization_id", "project_id") REFERENCES "projects" ("organization_id", "id") ON UPDATE NO ACTION ON DELETE CASCADE,
  CONSTRAINT "execution_selection_profiles_trigger_fkey" FOREIGN KEY ("organization_id", "project_id", "trigger_instance_id") REFERENCES "trigger_instances" ("organization_id", "project_id", "id") ON UPDATE NO ACTION ON DELETE CASCADE,
  CONSTRAINT "execution_selection_profiles_execution_context_check" CHECK (execution_context <> ''::text)
);
-- Create index "execution_selection_profiles_assistant_trigger_context_key" to table: "execution_selection_profiles"
CREATE UNIQUE INDEX "execution_selection_profiles_assistant_trigger_context_key" ON "execution_selection_profiles" ("assistant_id", "trigger_instance_id", "execution_context") WHERE (deleted IS FALSE);
-- Create index "execution_selection_profiles_organization_id_idx" to table: "execution_selection_profiles"
CREATE INDEX "execution_selection_profiles_organization_id_idx" ON "execution_selection_profiles" ("organization_id") WHERE (deleted IS FALSE);
-- Create index "execution_selection_profiles_project_id_idx" to table: "execution_selection_profiles"
CREATE INDEX "execution_selection_profiles_project_id_idx" ON "execution_selection_profiles" ("project_id");
-- Create index "execution_selection_profiles_tenant_key" to table: "execution_selection_profiles"
CREATE UNIQUE INDEX "execution_selection_profiles_tenant_key" ON "execution_selection_profiles" ("id", "organization_id", "project_id");
-- Create index "execution_selection_profiles_trigger_idx" to table: "execution_selection_profiles"
CREATE INDEX "execution_selection_profiles_trigger_idx" ON "execution_selection_profiles" ("organization_id", "project_id", "trigger_instance_id");
-- Set comment to column: "id" on table: "execution_selection_profiles"
COMMENT ON COLUMN "execution_selection_profiles"."id" IS '@access: confidential';
-- Set comment to column: "organization_id" on table: "execution_selection_profiles"
COMMENT ON COLUMN "execution_selection_profiles"."organization_id" IS '@access: confidential';
-- Set comment to column: "project_id" on table: "execution_selection_profiles"
COMMENT ON COLUMN "execution_selection_profiles"."project_id" IS '@access: confidential';
-- Set comment to column: "assistant_id" on table: "execution_selection_profiles"
COMMENT ON COLUMN "execution_selection_profiles"."assistant_id" IS '@access: confidential';
-- Set comment to column: "trigger_instance_id" on table: "execution_selection_profiles"
COMMENT ON COLUMN "execution_selection_profiles"."trigger_instance_id" IS '@access: confidential';
-- Set comment to column: "execution_context" on table: "execution_selection_profiles"
COMMENT ON COLUMN "execution_selection_profiles"."execution_context" IS '@access: confidential';
-- Set comment to column: "created_at" on table: "execution_selection_profiles"
COMMENT ON COLUMN "execution_selection_profiles"."created_at" IS '@access: confidential';
-- Set comment to column: "updated_at" on table: "execution_selection_profiles"
COMMENT ON COLUMN "execution_selection_profiles"."updated_at" IS '@access: confidential';
-- Set comment to column: "deleted_at" on table: "execution_selection_profiles"
COMMENT ON COLUMN "execution_selection_profiles"."deleted_at" IS '@access: confidential';
-- Set comment to column: "deleted" on table: "execution_selection_profiles"
COMMENT ON COLUMN "execution_selection_profiles"."deleted" IS '@access: confidential';
-- Create "execution_selection_profile_versions" table
CREATE TABLE "execution_selection_profile_versions" (
  "id" uuid NOT NULL DEFAULT generate_uuidv7(),
  "profile_id" uuid NOT NULL,
  "organization_id" text NOT NULL,
  "project_id" uuid NOT NULL,
  "version" bigint NOT NULL,
  "source" text NOT NULL,
  "created_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "updated_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  PRIMARY KEY ("id"),
  CONSTRAINT "execution_selection_profile_versions_profile_fkey" FOREIGN KEY ("profile_id", "organization_id", "project_id") REFERENCES "execution_selection_profiles" ("id", "organization_id", "project_id") ON UPDATE NO ACTION ON DELETE CASCADE,
  CONSTRAINT "execution_selection_profile_versions_source_check" CHECK (source <> ''::text),
  CONSTRAINT "execution_selection_profile_versions_version_check" CHECK (version > 0)
);
-- Create index "execution_selection_profile_versions_profile_version_key" to table: "execution_selection_profile_versions"
CREATE UNIQUE INDEX "execution_selection_profile_versions_profile_version_key" ON "execution_selection_profile_versions" ("profile_id", "version");
-- Create index "execution_selection_profile_versions_project_id_idx" to table: "execution_selection_profile_versions"
CREATE INDEX "execution_selection_profile_versions_project_id_idx" ON "execution_selection_profile_versions" ("project_id");
-- Set comment to column: "id" on table: "execution_selection_profile_versions"
COMMENT ON COLUMN "execution_selection_profile_versions"."id" IS '@access: confidential';
-- Set comment to column: "profile_id" on table: "execution_selection_profile_versions"
COMMENT ON COLUMN "execution_selection_profile_versions"."profile_id" IS '@access: confidential';
-- Set comment to column: "organization_id" on table: "execution_selection_profile_versions"
COMMENT ON COLUMN "execution_selection_profile_versions"."organization_id" IS '@access: confidential';
-- Set comment to column: "project_id" on table: "execution_selection_profile_versions"
COMMENT ON COLUMN "execution_selection_profile_versions"."project_id" IS '@access: confidential';
-- Set comment to column: "version" on table: "execution_selection_profile_versions"
COMMENT ON COLUMN "execution_selection_profile_versions"."version" IS '@access: confidential';
-- Set comment to column: "source" on table: "execution_selection_profile_versions"
COMMENT ON COLUMN "execution_selection_profile_versions"."source" IS '@access: confidential';
-- Set comment to column: "created_at" on table: "execution_selection_profile_versions"
COMMENT ON COLUMN "execution_selection_profile_versions"."created_at" IS '@access: confidential';
-- Set comment to column: "updated_at" on table: "execution_selection_profile_versions"
COMMENT ON COLUMN "execution_selection_profile_versions"."updated_at" IS '@access: confidential';
