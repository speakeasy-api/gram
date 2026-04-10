-- Create "corpus_annotations" table
CREATE TABLE "corpus_annotations" (
  "id" uuid NOT NULL DEFAULT generate_uuidv7(),
  "project_id" uuid NOT NULL,
  "organization_id" text NOT NULL,
  "file_path" text NOT NULL,
  "author_id" text NOT NULL,
  "author_type" text NOT NULL,
  "content" text NOT NULL,
  "line_start" integer NULL,
  "line_end" integer NULL,
  "created_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "updated_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "deleted_at" timestamptz NULL,
  "deleted" boolean NOT NULL GENERATED ALWAYS AS (deleted_at IS NOT NULL) STORED,
  PRIMARY KEY ("id"),
  CONSTRAINT "corpus_annotations_project_id_fkey" FOREIGN KEY ("project_id") REFERENCES "projects" ("id") ON UPDATE NO ACTION ON DELETE SET NULL,
  CONSTRAINT "corpus_annotations_author_type_check" CHECK (author_type = ANY (ARRAY['human'::text, 'agent'::text]))
);
-- Create index "corpus_annotations_project_id_file_path_idx" to table: "corpus_annotations"
CREATE INDEX "corpus_annotations_project_id_file_path_idx" ON "corpus_annotations" ("project_id", "file_path");
-- Create "corpus_auto_publish_configs" table
CREATE TABLE "corpus_auto_publish_configs" (
  "id" uuid NOT NULL DEFAULT generate_uuidv7(),
  "project_id" uuid NOT NULL,
  "organization_id" text NOT NULL,
  "enabled" boolean NOT NULL DEFAULT false,
  "interval_minutes" integer NOT NULL DEFAULT 10,
  "min_upvotes" integer NOT NULL DEFAULT 0,
  "author_type_filter" text NULL,
  "label_filter" jsonb NULL,
  "min_age_hours" integer NOT NULL DEFAULT 0,
  "created_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "updated_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  PRIMARY KEY ("id"),
  CONSTRAINT "corpus_auto_publish_configs_project_id_key" UNIQUE ("project_id"),
  CONSTRAINT "corpus_auto_publish_configs_project_id_fkey" FOREIGN KEY ("project_id") REFERENCES "projects" ("id") ON UPDATE NO ACTION ON DELETE SET NULL
);
-- Create "corpus_chunks" table
CREATE TABLE "corpus_chunks" (
  "id" uuid NOT NULL DEFAULT generate_uuidv7(),
  "project_id" uuid NOT NULL,
  "organization_id" text NOT NULL,
  "chunk_id" text NOT NULL,
  "file_path" text NOT NULL,
  "heading_path" text NULL,
  "breadcrumb" text NULL,
  "content" text NOT NULL,
  "content_text" text NOT NULL,
  "content_tsvector" tsvector NOT NULL GENERATED ALWAYS AS (to_tsvector('english'::regconfig, content_text)) STORED,
  "embedding" vector(3072) NULL,
  "metadata" jsonb NULL,
  "strategy" text NULL,
  "manifest_fingerprint" text NULL,
  "content_fingerprint" text NOT NULL,
  "created_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "updated_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  PRIMARY KEY ("id"),
  CONSTRAINT "corpus_chunks_project_id_chunk_id_key" UNIQUE ("project_id", "chunk_id"),
  CONSTRAINT "corpus_chunks_project_id_fkey" FOREIGN KEY ("project_id") REFERENCES "projects" ("id") ON UPDATE NO ACTION ON DELETE SET NULL
);
-- Create index "corpus_chunks_content_tsvector_idx" to table: "corpus_chunks"
CREATE INDEX "corpus_chunks_content_tsvector_idx" ON "corpus_chunks" USING gin ("content_tsvector");
-- Create "corpus_drafts" table
CREATE TABLE "corpus_drafts" (
  "id" uuid NOT NULL DEFAULT generate_uuidv7(),
  "project_id" uuid NOT NULL,
  "organization_id" text NOT NULL,
  "file_path" text NOT NULL,
  "content" text NULL,
  "operation" text NOT NULL,
  "status" text NOT NULL DEFAULT 'open',
  "source" text NULL,
  "author_type" text NULL,
  "labels" jsonb NULL,
  "commit_sha" text NULL,
  "created_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "updated_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "deleted_at" timestamptz NULL,
  "deleted" boolean NOT NULL GENERATED ALWAYS AS (deleted_at IS NOT NULL) STORED,
  PRIMARY KEY ("id"),
  CONSTRAINT "corpus_drafts_project_id_fkey" FOREIGN KEY ("project_id") REFERENCES "projects" ("id") ON UPDATE NO ACTION ON DELETE SET NULL,
  CONSTRAINT "corpus_drafts_file_path_check" CHECK (file_path <> ''::text),
  CONSTRAINT "corpus_drafts_operation_check" CHECK (operation = ANY (ARRAY['create'::text, 'update'::text, 'delete'::text])),
  CONSTRAINT "corpus_drafts_status_check" CHECK (status = ANY (ARRAY['open'::text, 'published'::text, 'rejected'::text]))
);
-- Create index "corpus_drafts_project_id_status_idx" to table: "corpus_drafts"
CREATE INDEX "corpus_drafts_project_id_status_idx" ON "corpus_drafts" ("project_id", "status");
-- Create "corpus_feedback" table
CREATE TABLE "corpus_feedback" (
  "id" uuid NOT NULL DEFAULT generate_uuidv7(),
  "project_id" uuid NOT NULL,
  "organization_id" text NOT NULL,
  "file_path" text NOT NULL,
  "user_id" text NOT NULL,
  "direction" text NOT NULL,
  "labels" jsonb NULL,
  "created_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "updated_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  PRIMARY KEY ("id"),
  CONSTRAINT "corpus_feedback_project_id_file_path_user_id_key" UNIQUE ("project_id", "file_path", "user_id"),
  CONSTRAINT "corpus_feedback_project_id_fkey" FOREIGN KEY ("project_id") REFERENCES "projects" ("id") ON UPDATE NO ACTION ON DELETE SET NULL,
  CONSTRAINT "corpus_feedback_direction_check" CHECK (direction = ANY (ARRAY['up'::text, 'down'::text]))
);
-- Create "corpus_feedback_comments" table
CREATE TABLE "corpus_feedback_comments" (
  "id" uuid NOT NULL DEFAULT generate_uuidv7(),
  "project_id" uuid NOT NULL,
  "organization_id" text NOT NULL,
  "file_path" text NOT NULL,
  "author_id" text NOT NULL,
  "author_type" text NOT NULL,
  "content" text NOT NULL,
  "created_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "updated_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "deleted_at" timestamptz NULL,
  "deleted" boolean NOT NULL GENERATED ALWAYS AS (deleted_at IS NOT NULL) STORED,
  PRIMARY KEY ("id"),
  CONSTRAINT "corpus_feedback_comments_project_id_fkey" FOREIGN KEY ("project_id") REFERENCES "projects" ("id") ON UPDATE NO ACTION ON DELETE SET NULL,
  CONSTRAINT "corpus_feedback_comments_author_type_check" CHECK (author_type = ANY (ARRAY['human'::text, 'agent'::text]))
);
-- Create index "corpus_feedback_comments_project_id_file_path_idx" to table: "corpus_feedback_comments"
CREATE INDEX "corpus_feedback_comments_project_id_file_path_idx" ON "corpus_feedback_comments" ("project_id", "file_path");
-- Create "corpus_index_state" table
CREATE TABLE "corpus_index_state" (
  "id" uuid NOT NULL DEFAULT generate_uuidv7(),
  "project_id" uuid NOT NULL,
  "organization_id" text NOT NULL,
  "last_indexed_sha" text NULL,
  "embedding_model" text NULL,
  "created_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "updated_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  PRIMARY KEY ("id"),
  CONSTRAINT "corpus_index_state_project_id_key" UNIQUE ("project_id"),
  CONSTRAINT "corpus_index_state_project_id_fkey" FOREIGN KEY ("project_id") REFERENCES "projects" ("id") ON UPDATE NO ACTION ON DELETE SET NULL
);
-- Create "corpus_publish_events" table
CREATE TABLE "corpus_publish_events" (
  "id" uuid NOT NULL DEFAULT generate_uuidv7(),
  "project_id" uuid NOT NULL,
  "organization_id" text NOT NULL,
  "commit_sha" text NOT NULL,
  "status" text NOT NULL DEFAULT 'pending',
  "created_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "updated_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  PRIMARY KEY ("id"),
  CONSTRAINT "corpus_publish_events_project_id_fkey" FOREIGN KEY ("project_id") REFERENCES "projects" ("id") ON UPDATE NO ACTION ON DELETE SET NULL,
  CONSTRAINT "corpus_publish_events_status_check" CHECK (status = ANY (ARRAY['pending'::text, 'indexing'::text, 'indexed'::text, 'failed'::text]))
);
-- Create index "corpus_publish_events_project_id_status_idx" to table: "corpus_publish_events"
CREATE INDEX "corpus_publish_events_project_id_status_idx" ON "corpus_publish_events" ("project_id", "status");
