-- Create "skill_version_files" table
CREATE TABLE "skill_version_files" (
  "skill_version_id" uuid NOT NULL,
  "skill_id" uuid NOT NULL,
  "project_id" uuid NOT NULL,
  "path" text NOT NULL,
  "content" bytea NOT NULL,
  "sha256" text NOT NULL,
  "size_bytes" bigint NOT NULL,
  "content_type" text NOT NULL,
  "created_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  PRIMARY KEY ("skill_version_id", "path"),
  CONSTRAINT "skill_version_files_project_id_skill_id_fkey" FOREIGN KEY ("project_id", "skill_id") REFERENCES "skills" ("project_id", "id") ON UPDATE NO ACTION ON DELETE CASCADE,
  CONSTRAINT "skill_version_files_skill_id_skill_version_id_fkey" FOREIGN KEY ("skill_id", "skill_version_id") REFERENCES "skill_versions" ("skill_id", "id") ON UPDATE NO ACTION ON DELETE CASCADE,
  CONSTRAINT "skill_version_files_content_size_check" CHECK (octet_length(content) <= 5242880),
  CONSTRAINT "skill_version_files_path_check" CHECK ((path <> ''::text) AND (char_length(path) <= 512))
);
-- Create index "skill_version_files_project_id_skill_id_idx" to table: "skill_version_files"
CREATE INDEX "skill_version_files_project_id_skill_id_idx" ON "skill_version_files" ("project_id", "skill_id");
-- Set comment to column: "skill_version_id" on table: "skill_version_files"
COMMENT ON COLUMN "skill_version_files"."skill_version_id" IS '@access: confidential';
-- Set comment to column: "skill_id" on table: "skill_version_files"
COMMENT ON COLUMN "skill_version_files"."skill_id" IS '@access: confidential';
-- Set comment to column: "project_id" on table: "skill_version_files"
COMMENT ON COLUMN "skill_version_files"."project_id" IS '@access: confidential';
-- Set comment to column: "path" on table: "skill_version_files"
COMMENT ON COLUMN "skill_version_files"."path" IS 'Slash-separated path relative to the skill root, for example references/REFERENCE.md.
@access: opaque-confidential';
-- Set comment to column: "content" on table: "skill_version_files"
COMMENT ON COLUMN "skill_version_files"."content" IS 'Exact file bytes. May be executable code or binary data.
@access: opaque-restricted';
-- Set comment to column: "sha256" on table: "skill_version_files"
COMMENT ON COLUMN "skill_version_files"."sha256" IS 'Lowercase hex SHA-256 of content.
@access: confidential';
-- Set comment to column: "size_bytes" on table: "skill_version_files"
COMMENT ON COLUMN "skill_version_files"."size_bytes" IS '@access: confidential';
-- Set comment to column: "content_type" on table: "skill_version_files"
COMMENT ON COLUMN "skill_version_files"."content_type" IS '@access: confidential';
-- Set comment to column: "created_at" on table: "skill_version_files"
COMMENT ON COLUMN "skill_version_files"."created_at" IS '@access: confidential';
