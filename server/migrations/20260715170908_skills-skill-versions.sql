-- Create "skills" table
CREATE TABLE "skills" (
  "id" uuid NOT NULL DEFAULT generate_uuidv7(),
  "project_id" uuid NOT NULL,
  "name" text NOT NULL,
  "display_name" text NOT NULL,
  "summary" text NULL,
  "source_kind" text NOT NULL DEFAULT 'manual',
  "classification" text NOT NULL DEFAULT 'custom',
  "archived_at" timestamptz NULL,
  "created_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "updated_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  PRIMARY KEY ("id"),
  CONSTRAINT "skills_project_id_fkey" FOREIGN KEY ("project_id") REFERENCES "projects" ("id") ON UPDATE NO ACTION ON DELETE CASCADE
);
-- Create index "skills_project_id_idx" to table: "skills"
CREATE INDEX "skills_project_id_idx" ON "skills" ("project_id");
-- Create index "skills_project_id_name_key" to table: "skills"
CREATE UNIQUE INDEX "skills_project_id_name_key" ON "skills" ("project_id", "name") WHERE (archived_at IS NULL);
-- Create "skill_versions" table
CREATE TABLE "skill_versions" (
  "id" uuid NOT NULL DEFAULT generate_uuidv7(),
  "skill_id" uuid NOT NULL,
  "content" text NOT NULL,
  "canonical_sha256" text NOT NULL,
  "raw_sha256" text NOT NULL,
  "description" text NULL,
  "metadata" jsonb NOT NULL DEFAULT '{}',
  "spec_valid" boolean NOT NULL,
  "validation_errors" jsonb NOT NULL DEFAULT '[]',
  "created_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "created_by_user_id" text NOT NULL,
  PRIMARY KEY ("id"),
  CONSTRAINT "skill_versions_skill_id_fkey" FOREIGN KEY ("skill_id") REFERENCES "skills" ("id") ON UPDATE NO ACTION ON DELETE CASCADE,
  CONSTRAINT "skill_versions_content_size_check" CHECK (octet_length(content) <= 65536)
);
-- Create index "skill_versions_skill_id_canonical_sha256_key" to table: "skill_versions"
CREATE UNIQUE INDEX "skill_versions_skill_id_canonical_sha256_key" ON "skill_versions" ("skill_id", "canonical_sha256");
