-- Modify "skill_versions" table
ALTER TABLE "skill_versions" DROP CONSTRAINT "skill_versions_asset_format_check", ADD CONSTRAINT "skill_versions_asset_format_check" CHECK (asset_format = 'zip'::text);
-- Create "skills_capture_policies" table
CREATE TABLE "skills_capture_policies" (
  "id" uuid NOT NULL DEFAULT generate_uuidv7(),
  "organization_id" text NOT NULL,
  "project_id" uuid NULL,
  "mode" text NOT NULL,
  "created_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "updated_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "deleted_at" timestamptz NULL,
  "deleted" boolean NOT NULL GENERATED ALWAYS AS (deleted_at IS NOT NULL) STORED,
  PRIMARY KEY ("id"),
  CONSTRAINT "skills_capture_policies_project_id_fkey" FOREIGN KEY ("project_id") REFERENCES "projects" ("id") ON UPDATE NO ACTION ON DELETE CASCADE,
  CONSTRAINT "skills_capture_policies_mode_check" CHECK (mode = ANY (ARRAY['disabled'::text, 'project_only'::text, 'user_only'::text, 'project_and_user'::text])),
  CONSTRAINT "skills_capture_policies_scope_check" CHECK (((project_id IS NULL) AND (organization_id <> ''::text)) OR ((project_id IS NOT NULL) AND (organization_id <> ''::text)))
);
-- Create index "skills_capture_policies_org_default_key" to table: "skills_capture_policies"
CREATE UNIQUE INDEX "skills_capture_policies_org_default_key" ON "skills_capture_policies" ("organization_id") WHERE ((project_id IS NULL) AND (deleted IS FALSE));
-- Create index "skills_capture_policies_project_override_key" to table: "skills_capture_policies"
CREATE UNIQUE INDEX "skills_capture_policies_project_override_key" ON "skills_capture_policies" ("organization_id", "project_id") WHERE ((project_id IS NOT NULL) AND (deleted IS FALSE));
