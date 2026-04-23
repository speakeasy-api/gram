-- Create "skills" table
CREATE TABLE "skills" (
  "created_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "deleted_at" timestamptz NULL,
  "updated_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "skill_uuid" text NULL,
  "slug" text NOT NULL,
  "description" text NULL,
  "state" text NOT NULL,
  "created_by_user_id" text NOT NULL,
  "name" text NOT NULL,
  "organization_id" text NOT NULL,
  "id" uuid NOT NULL DEFAULT generate_uuidv7(),
  "active_version_id" uuid NULL,
  "project_id" uuid NOT NULL,
  "deleted" boolean NOT NULL GENERATED ALWAYS AS (deleted_at IS NOT NULL) STORED,
  PRIMARY KEY ("id"),
  CONSTRAINT "skills_name_check" CHECK ((name <> ''::text) AND (char_length(name) <= 100)),
  CONSTRAINT "skills_slug_check" CHECK ((slug <> ''::text) AND (char_length(slug) <= 100)),
  CONSTRAINT "skills_description_check" CHECK ((description <> ''::text) AND (char_length(description) <= 2000)),
  CONSTRAINT "skills_skill_uuid_check" CHECK ((skill_uuid <> ''::text) AND (char_length(skill_uuid) <= 100)),
  CONSTRAINT "skills_state_check" CHECK (state = ANY (ARRAY['pending_review'::text, 'published'::text, 'archived'::text])),
  CONSTRAINT "skills_project_id_fkey" FOREIGN KEY ("project_id") REFERENCES "projects" ("id") ON UPDATE NO ACTION ON DELETE CASCADE
);
-- Create index "skills_project_id_slug_key" to table: "skills"
CREATE UNIQUE INDEX "skills_project_id_slug_key" ON "skills" ("project_id", "slug") WHERE (deleted IS FALSE);
-- Create index "skills_project_id_skill_uuid_key" to table: "skills"
CREATE UNIQUE INDEX "skills_project_id_skill_uuid_key" ON "skills" ("project_id", "skill_uuid") WHERE ((skill_uuid IS NOT NULL) AND (deleted IS FALSE));

-- Create "skill_versions" table
CREATE TABLE "skill_versions" (
  "size_bytes" bigint NOT NULL,
  "updated_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "created_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "first_seen_at" timestamptz NULL,
  "skill_bytes" bigint NULL,
  "content_sha256" text NOT NULL,
  "asset_format" text NOT NULL,
  "state" text NOT NULL,
  "captured_by_user_id" text NOT NULL,
  "author_name" text NULL,
  "first_seen_trace_id" text NULL,
  "first_seen_session_id" text NULL,
  "id" uuid NOT NULL DEFAULT generate_uuidv7(),
  "asset_id" uuid NOT NULL,
  "skill_id" uuid NOT NULL,
  PRIMARY KEY ("id"),
  CONSTRAINT "skill_versions_asset_format_check" CHECK (asset_format = ANY (ARRAY['zip'::text])),
  CONSTRAINT "skill_versions_size_bytes_check" CHECK (size_bytes >= 0),
  CONSTRAINT "skill_versions_skill_bytes_check" CHECK (skill_bytes >= 0),
  CONSTRAINT "skill_versions_state_check" CHECK (state = ANY (ARRAY['pending_review'::text, 'active'::text, 'superseded'::text])),
  CONSTRAINT "skill_versions_author_name_check" CHECK ((author_name <> ''::text) AND (char_length(author_name) <= 255)),
  CONSTRAINT "skill_versions_first_seen_trace_id_check" CHECK ((first_seen_trace_id <> ''::text) AND (char_length(first_seen_trace_id) <= 100)),
  CONSTRAINT "skill_versions_first_seen_session_id_check" CHECK ((first_seen_session_id <> ''::text) AND (char_length(first_seen_session_id) <= 100)),
  CONSTRAINT "skill_versions_skill_id_fkey" FOREIGN KEY ("skill_id") REFERENCES "skills" ("id") ON UPDATE NO ACTION ON DELETE CASCADE,
  CONSTRAINT "skill_versions_asset_id_fkey" FOREIGN KEY ("asset_id") REFERENCES "assets" ("id") ON UPDATE NO ACTION ON DELETE CASCADE,
  CONSTRAINT "skill_versions_skill_id_content_sha256_key" UNIQUE ("skill_id", "content_sha256")
);
-- Create index "skill_versions_active_skill_id_key" to table: "skill_versions"
CREATE UNIQUE INDEX "skill_versions_active_skill_id_key" ON "skill_versions" ("skill_id") WHERE (state = 'active'::text);
-- Create index "skill_versions_skill_id_created_at_idx" to table: "skill_versions"
CREATE INDEX "skill_versions_skill_id_created_at_idx" ON "skill_versions" ("skill_id", "created_at" DESC);

-- Modify "skills" table
ALTER TABLE "skills" ADD CONSTRAINT "skills_active_version_id_fkey" FOREIGN KEY ("active_version_id") REFERENCES "skill_versions" ("id") ON UPDATE NO ACTION ON DELETE SET NULL;
