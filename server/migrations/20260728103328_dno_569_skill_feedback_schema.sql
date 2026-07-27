-- Create "skill_edit_suggestions" table
CREATE TABLE "skill_edit_suggestions" (
  "id" uuid NOT NULL DEFAULT generate_uuidv7(),
  "project_id" uuid NOT NULL,
  "skill_id" uuid NOT NULL,
  "base_version_id" uuid NOT NULL,
  "rationale" text NOT NULL,
  "status" text NOT NULL DEFAULT 'open',
  "scored_session_count" bigint NOT NULL DEFAULT 0,
  "approved_by_user_id" text NULL,
  "approved_at" timestamptz NULL,
  "created_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "updated_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  PRIMARY KEY ("id"),
  CONSTRAINT "skill_edit_suggestions_project_id_fkey" FOREIGN KEY ("project_id") REFERENCES "projects" ("id") ON UPDATE NO ACTION ON DELETE CASCADE,
  CONSTRAINT "skill_edit_suggestions_project_id_skill_id_fkey" FOREIGN KEY ("project_id", "skill_id") REFERENCES "skills" ("project_id", "id") ON UPDATE NO ACTION ON DELETE NO ACTION,
  CONSTRAINT "skill_edit_suggestions_skill_id_base_version_id_fkey" FOREIGN KEY ("skill_id", "base_version_id") REFERENCES "skill_versions" ("skill_id", "id") ON UPDATE NO ACTION ON DELETE NO ACTION
);
-- Create index "skill_edit_suggestions_project_id_id_key" to table: "skill_edit_suggestions"
CREATE UNIQUE INDEX "skill_edit_suggestions_project_id_id_key" ON "skill_edit_suggestions" ("project_id", "id");
-- Create index "skill_edit_suggestions_project_id_skill_id_created_at_idx" to table: "skill_edit_suggestions"
CREATE INDEX "skill_edit_suggestions_project_id_skill_id_created_at_idx" ON "skill_edit_suggestions" ("project_id", "skill_id", "created_at" DESC);
-- Create index "skill_edit_suggestions_skill_id_open_key" to table: "skill_edit_suggestions"
CREATE UNIQUE INDEX "skill_edit_suggestions_skill_id_open_key" ON "skill_edit_suggestions" ("skill_id") WHERE (status = 'open'::text);
-- Create "skill_edit_suggestion_changes" table
CREATE TABLE "skill_edit_suggestion_changes" (
  "id" uuid NOT NULL DEFAULT generate_uuidv7(),
  "project_id" uuid NOT NULL,
  "suggestion_id" uuid NOT NULL,
  "proposed_diff" text NOT NULL,
  "rationale" text NOT NULL,
  "position" integer NOT NULL,
  "created_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "updated_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  PRIMARY KEY ("id"),
  CONSTRAINT "skill_edit_suggestion_changes_project_id_suggestion_id_fkey" FOREIGN KEY ("project_id", "suggestion_id") REFERENCES "skill_edit_suggestions" ("project_id", "id") ON UPDATE NO ACTION ON DELETE CASCADE,
  CONSTRAINT "skill_edit_suggestion_changes_proposed_diff_size_check" CHECK (octet_length(proposed_diff) <= 131072)
);
-- Create index "skill_edit_suggestion_changes_project_id_id_key" to table: "skill_edit_suggestion_changes"
CREATE UNIQUE INDEX "skill_edit_suggestion_changes_project_id_id_key" ON "skill_edit_suggestion_changes" ("project_id", "id");
-- Create index "skill_edit_suggestion_changes_suggestion_position_idx" to table: "skill_edit_suggestion_changes"
CREATE INDEX "skill_edit_suggestion_changes_suggestion_position_idx" ON "skill_edit_suggestion_changes" ("project_id", "suggestion_id", "position");
-- Create "skill_feedback" table
CREATE TABLE "skill_feedback" (
  "id" uuid NOT NULL DEFAULT generate_uuidv7(),
  "project_id" uuid NOT NULL,
  "skill_id" uuid NULL,
  "skill_version_id" uuid NULL,
  "skill_name" text NOT NULL,
  "source" text NOT NULL,
  "outcome" text NOT NULL,
  "note" text NULL,
  "session_id" text NULL,
  "user_id" text NULL,
  "user_email" text NULL,
  "reviewed_at" timestamptz NULL,
  "created_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  PRIMARY KEY ("id"),
  CONSTRAINT "skill_feedback_project_id_fkey" FOREIGN KEY ("project_id") REFERENCES "projects" ("id") ON UPDATE NO ACTION ON DELETE CASCADE,
  CONSTRAINT "skill_feedback_project_id_skill_id_fkey" FOREIGN KEY ("project_id", "skill_id") REFERENCES "skills" ("project_id", "id") ON UPDATE NO ACTION ON DELETE NO ACTION,
  CONSTRAINT "skill_feedback_skill_id_skill_version_id_fkey" FOREIGN KEY ("skill_id", "skill_version_id") REFERENCES "skill_versions" ("skill_id", "id") ON UPDATE NO ACTION ON DELETE NO ACTION,
  CONSTRAINT "skill_feedback_note_size_check" CHECK ((note <> ''::text) AND (char_length(note) <= 4000)),
  CONSTRAINT "skill_feedback_skill_id_skill_version_id_check" CHECK ((skill_version_id IS NULL) OR (skill_id IS NOT NULL))
);
-- Create index "skill_feedback_project_id_id_key" to table: "skill_feedback"
CREATE UNIQUE INDEX "skill_feedback_project_id_id_key" ON "skill_feedback" ("project_id", "id");
-- Create index "skill_feedback_project_id_skill_name_created_at_id_idx" to table: "skill_feedback"
CREATE INDEX "skill_feedback_project_id_skill_name_created_at_id_idx" ON "skill_feedback" ("project_id", "skill_name", "created_at" DESC, "id" DESC);
-- Create index "skill_feedback_project_id_skill_name_created_at_unreviewed_idx" to table: "skill_feedback"
CREATE INDEX "skill_feedback_project_id_skill_name_created_at_unreviewed_idx" ON "skill_feedback" ("project_id", "skill_name", "created_at") WHERE (reviewed_at IS NULL);
-- Create "skill_edit_suggestion_feedback" table
CREATE TABLE "skill_edit_suggestion_feedback" (
  "project_id" uuid NOT NULL,
  "change_id" uuid NOT NULL,
  "feedback_id" uuid NOT NULL,
  "created_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  PRIMARY KEY ("change_id", "feedback_id"),
  CONSTRAINT "skill_edit_suggestion_feedback_project_id_change_id_fkey" FOREIGN KEY ("project_id", "change_id") REFERENCES "skill_edit_suggestion_changes" ("project_id", "id") ON UPDATE NO ACTION ON DELETE CASCADE,
  CONSTRAINT "skill_edit_suggestion_feedback_project_id_feedback_id_fkey" FOREIGN KEY ("project_id", "feedback_id") REFERENCES "skill_feedback" ("project_id", "id") ON UPDATE NO ACTION ON DELETE CASCADE
);
-- Create index "skill_edit_suggestion_feedback_project_id_feedback_id_idx" to table: "skill_edit_suggestion_feedback"
CREATE INDEX "skill_edit_suggestion_feedback_project_id_feedback_id_idx" ON "skill_edit_suggestion_feedback" ("project_id", "feedback_id");
