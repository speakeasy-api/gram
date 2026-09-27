-- Create "queries" table
CREATE TABLE "queries" (
  "id" uuid NOT NULL DEFAULT generate_uuidv7(),
  "project_id" uuid NOT NULL,
  "organization_id" text NOT NULL,
  "created_by_user_id" text NULL,
  "name" text NOT NULL,
  "dataset" text NOT NULL,
  "spec" jsonb NOT NULL,
  "created_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "updated_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "deleted_at" timestamptz NULL,
  "deleted" boolean NOT NULL GENERATED ALWAYS AS (deleted_at IS NOT NULL) STORED,
  PRIMARY KEY ("id"),
  CONSTRAINT "queries_project_id_fkey" FOREIGN KEY ("project_id") REFERENCES "projects" ("id") ON UPDATE NO ACTION ON DELETE CASCADE,
  CONSTRAINT "queries_dataset_check" CHECK (dataset <> ''::text),
  CONSTRAINT "queries_name_check" CHECK ((name <> ''::text) AND (char_length(name) <= 200))
);
-- Create index "queries_project_id_updated_at_idx" to table: "queries"
CREATE INDEX "queries_project_id_updated_at_idx" ON "queries" ("project_id", "updated_at" DESC) WHERE (deleted IS FALSE);
