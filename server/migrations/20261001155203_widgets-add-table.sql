-- Create "widgets" table
CREATE TABLE "widgets" (
  "id" uuid NOT NULL DEFAULT generate_uuidv7(),
  "project_id" uuid NOT NULL,
  "organization_id" text NOT NULL,
  "created_by_user_id" text NULL,
  "name" text NOT NULL,
  "description" text NULL,
  "dataset" text NOT NULL,
  "query" jsonb NOT NULL,
  "visualization" jsonb NOT NULL,
  "created_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "updated_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "deleted_at" timestamptz NULL,
  "deleted" boolean NOT NULL GENERATED ALWAYS AS (deleted_at IS NOT NULL) STORED,
  PRIMARY KEY ("id"),
  CONSTRAINT "widgets_organization_id_project_id_fkey" FOREIGN KEY ("organization_id", "project_id") REFERENCES "projects" ("organization_id", "id") ON UPDATE NO ACTION ON DELETE CASCADE,
  CONSTRAINT "widgets_dataset_check" CHECK (dataset <> ''::text),
  CONSTRAINT "widgets_description_check" CHECK (char_length(description) <= 2000),
  CONSTRAINT "widgets_name_check" CHECK ((name <> ''::text) AND (char_length(name) <= 200))
);
-- Create index "widgets_project_id_updated_at_idx" to table: "widgets"
CREATE INDEX "widgets_project_id_updated_at_idx" ON "widgets" ("project_id", "updated_at" DESC) WHERE (deleted IS FALSE);
