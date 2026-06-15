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
  "rejected_by_user_id" text NULL,
  "rejected_reason" text NULL,
  "rejected_at" timestamptz NULL,
  "first_seen_trace_id" text NULL,
  "first_seen_session_id" text NULL,
  "id" uuid NOT NULL DEFAULT generate_uuidv7(),
  "asset_id" uuid NOT NULL,
  "skill_id" uuid NOT NULL,
  PRIMARY KEY ("id"),
  CONSTRAINT "skill_versions_skill_id_content_sha256_key" UNIQUE ("skill_id", "content_sha256"),
  CONSTRAINT "skill_versions_asset_format_check" CHECK (asset_format = 'zip'::text),
  CONSTRAINT "skill_versions_author_name_check" CHECK ((author_name <> ''::text) AND (char_length(author_name) <= 255)),
  CONSTRAINT "skill_versions_first_seen_session_id_check" CHECK ((first_seen_session_id <> ''::text) AND (char_length(first_seen_session_id) <= 100)),
  CONSTRAINT "skill_versions_first_seen_trace_id_check" CHECK ((first_seen_trace_id <> ''::text) AND (char_length(first_seen_trace_id) <= 100)),
  CONSTRAINT "skill_versions_rejected_by_user_id_check" CHECK ((rejected_by_user_id <> ''::text) AND (char_length(rejected_by_user_id) <= 255)),
  CONSTRAINT "skill_versions_rejected_fields_check" CHECK (((state = 'rejected'::text) AND (rejected_by_user_id IS NOT NULL) AND (rejected_reason IS NOT NULL) AND (rejected_at IS NOT NULL)) OR ((state <> 'rejected'::text) AND (rejected_by_user_id IS NULL) AND (rejected_reason IS NULL) AND (rejected_at IS NULL))),
  CONSTRAINT "skill_versions_rejected_reason_check" CHECK ((rejected_reason <> ''::text) AND (char_length(rejected_reason) <= 2000)),
  CONSTRAINT "skill_versions_size_bytes_check" CHECK (size_bytes >= 0),
  CONSTRAINT "skill_versions_skill_bytes_check" CHECK (skill_bytes >= 0),
  CONSTRAINT "skill_versions_state_check" CHECK (state = ANY (ARRAY['pending_review'::text, 'active'::text, 'superseded'::text, 'rejected'::text]))
);
-- Create index "skill_versions_active_skill_id_key" to table: "skill_versions"
CREATE UNIQUE INDEX "skill_versions_active_skill_id_key" ON "skill_versions" ("skill_id") WHERE (state = 'active'::text);
-- Create index "skill_versions_skill_id_created_at_idx" to table: "skill_versions"
CREATE INDEX "skill_versions_skill_id_created_at_idx" ON "skill_versions" ("skill_id", "created_at" DESC);
-- Create "skills" table
CREATE TABLE "skills" (
  "created_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "deleted_at" timestamptz NULL,
  "updated_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "skill_uuid" text NULL,
  "slug" text NOT NULL,
  "description" text NULL,
  "created_by_user_id" text NOT NULL,
  "name" text NOT NULL,
  "organization_id" text NOT NULL,
  "id" uuid NOT NULL DEFAULT generate_uuidv7(),
  "active_version_id" uuid NULL,
  "project_id" uuid NOT NULL,
  "deleted" boolean NOT NULL GENERATED ALWAYS AS (deleted_at IS NOT NULL) STORED,
  PRIMARY KEY ("id"),
  CONSTRAINT "skills_description_check" CHECK ((description <> ''::text) AND (char_length(description) <= 2000)),
  CONSTRAINT "skills_name_check" CHECK ((name <> ''::text) AND (char_length(name) <= 100)),
  CONSTRAINT "skills_skill_uuid_check" CHECK ((skill_uuid <> ''::text) AND (char_length(skill_uuid) <= 100)),
  CONSTRAINT "skills_slug_check" CHECK ((slug <> ''::text) AND (char_length(slug) <= 100))
);
-- Create index "skills_project_id_skill_uuid_key" to table: "skills"
CREATE UNIQUE INDEX "skills_project_id_skill_uuid_key" ON "skills" ("project_id", "skill_uuid") WHERE ((skill_uuid IS NOT NULL) AND (deleted IS FALSE));
-- Create index "skills_project_id_slug_key" to table: "skills"
CREATE UNIQUE INDEX "skills_project_id_slug_key" ON "skills" ("project_id", "slug") WHERE (deleted IS FALSE);
-- Create "skills_capture_attempts" table
CREATE TABLE "skills_capture_attempts" (
  "id" uuid NOT NULL DEFAULT generate_uuidv7(),
  "organization_id" text NOT NULL,
  "project_id" uuid NOT NULL,
  "captured_by_user_id" text NOT NULL,
  "skill_name" text NULL,
  "skill_slug" text NULL,
  "scope" text NOT NULL,
  "discovery_root" text NOT NULL,
  "source_type" text NOT NULL,
  "resolution_status" text NOT NULL,
  "content_sha256" text NULL,
  "asset_format" text NULL,
  "content_length" bigint NULL,
  "outcome" text NOT NULL,
  "reason" text NOT NULL,
  "skill_id" uuid NULL,
  "skill_version_id" uuid NULL,
  "asset_id" uuid NULL,
  "created_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "updated_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "deleted_at" timestamptz NULL,
  "deleted" boolean NOT NULL GENERATED ALWAYS AS (deleted_at IS NOT NULL) STORED,
  PRIMARY KEY ("id"),
  CONSTRAINT "skills_capture_attempts_asset_format_check" CHECK (asset_format = 'zip'::text),
  CONSTRAINT "skills_capture_attempts_content_length_check" CHECK (content_length >= 0),
  CONSTRAINT "skills_capture_attempts_content_sha256_check" CHECK (content_sha256 ~ '^[a-fA-F0-9]{64}$'::text),
  CONSTRAINT "skills_capture_attempts_discovery_root_check" CHECK ((discovery_root <> ''::text) AND (char_length(discovery_root) <= 60)),
  CONSTRAINT "skills_capture_attempts_outcome_check" CHECK (outcome = ANY (ARRAY['accepted'::text, 'duplicate'::text, 'rejected'::text])),
  CONSTRAINT "skills_capture_attempts_reason_check" CHECK ((reason <> ''::text) AND (char_length(reason) <= 100)),
  CONSTRAINT "skills_capture_attempts_resolution_status_check" CHECK ((resolution_status <> ''::text) AND (char_length(resolution_status) <= 60)),
  CONSTRAINT "skills_capture_attempts_scope_check" CHECK (scope = ANY (ARRAY['project'::text, 'user'::text])),
  CONSTRAINT "skills_capture_attempts_skill_name_check" CHECK ((skill_name <> ''::text) AND (char_length(skill_name) <= 100)),
  CONSTRAINT "skills_capture_attempts_skill_slug_check" CHECK ((skill_slug <> ''::text) AND (char_length(skill_slug) <= 100)),
  CONSTRAINT "skills_capture_attempts_source_type_check" CHECK ((source_type <> ''::text) AND (char_length(source_type) <= 60))
);
-- Create index "skills_capture_attempts_project_id_created_at_idx" to table: "skills_capture_attempts"
CREATE INDEX "skills_capture_attempts_project_id_created_at_idx" ON "skills_capture_attempts" ("project_id", "created_at" DESC) WHERE (deleted IS FALSE);
-- Create index "skills_capture_attempts_project_id_skill_slug_created_at_idx" to table: "skills_capture_attempts"
CREATE INDEX "skills_capture_attempts_project_id_skill_slug_created_at_idx" ON "skills_capture_attempts" ("project_id", "skill_slug", "created_at" DESC) WHERE ((deleted IS FALSE) AND (skill_slug IS NOT NULL));
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
  CONSTRAINT "skills_capture_policies_mode_check" CHECK (mode = ANY (ARRAY['disabled'::text, 'project_only'::text, 'user_only'::text, 'project_and_user'::text])),
  CONSTRAINT "skills_capture_policies_scope_check" CHECK (((project_id IS NULL) AND (organization_id <> ''::text)) OR ((project_id IS NOT NULL) AND (organization_id <> ''::text)))
);
-- Create index "skills_capture_policies_org_default_key" to table: "skills_capture_policies"
CREATE UNIQUE INDEX "skills_capture_policies_org_default_key" ON "skills_capture_policies" ("organization_id") WHERE ((project_id IS NULL) AND (deleted IS FALSE));
-- Create index "skills_capture_policies_project_override_key" to table: "skills_capture_policies"
CREATE UNIQUE INDEX "skills_capture_policies_project_override_key" ON "skills_capture_policies" ("organization_id", "project_id") WHERE ((project_id IS NOT NULL) AND (deleted IS FALSE));
-- Modify "skill_versions" table
ALTER TABLE "skill_versions" ADD CONSTRAINT "skill_versions_asset_id_fkey" FOREIGN KEY ("asset_id") REFERENCES "assets" ("id") ON UPDATE NO ACTION ON DELETE CASCADE, ADD CONSTRAINT "skill_versions_skill_id_fkey" FOREIGN KEY ("skill_id") REFERENCES "skills" ("id") ON UPDATE NO ACTION ON DELETE CASCADE;
-- Modify "skills" table
ALTER TABLE "skills" ADD CONSTRAINT "skills_active_version_id_fkey" FOREIGN KEY ("active_version_id") REFERENCES "skill_versions" ("id") ON UPDATE NO ACTION ON DELETE SET NULL, ADD CONSTRAINT "skills_project_id_fkey" FOREIGN KEY ("project_id") REFERENCES "projects" ("id") ON UPDATE NO ACTION ON DELETE CASCADE;
-- Modify "skills_capture_attempts" table
ALTER TABLE "skills_capture_attempts" ADD CONSTRAINT "skills_capture_attempts_asset_id_fkey" FOREIGN KEY ("asset_id") REFERENCES "assets" ("id") ON UPDATE NO ACTION ON DELETE SET NULL, ADD CONSTRAINT "skills_capture_attempts_project_id_fkey" FOREIGN KEY ("project_id") REFERENCES "projects" ("id") ON UPDATE NO ACTION ON DELETE CASCADE, ADD CONSTRAINT "skills_capture_attempts_skill_id_fkey" FOREIGN KEY ("skill_id") REFERENCES "skills" ("id") ON UPDATE NO ACTION ON DELETE SET NULL, ADD CONSTRAINT "skills_capture_attempts_skill_version_id_fkey" FOREIGN KEY ("skill_version_id") REFERENCES "skill_versions" ("id") ON UPDATE NO ACTION ON DELETE SET NULL;
-- Modify "skills_capture_policies" table
ALTER TABLE "skills_capture_policies" ADD CONSTRAINT "skills_capture_policies_project_id_fkey" FOREIGN KEY ("project_id") REFERENCES "projects" ("id") ON UPDATE NO ACTION ON DELETE CASCADE;
