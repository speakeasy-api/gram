-- Create "explore_saved_queries" table
CREATE TABLE "explore_saved_queries" (
  "id" uuid NOT NULL DEFAULT generate_uuidv7(),
  "organization_id" text NOT NULL,
  "name" text NOT NULL,
  "chart_type" text NOT NULL,
  "time_window" text NOT NULL,
  "spec" jsonb NOT NULL,
  "created_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "updated_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "deleted_at" timestamptz NULL,
  "deleted" boolean NOT NULL GENERATED ALWAYS AS (deleted_at IS NOT NULL) STORED,
  PRIMARY KEY ("id"),
  CONSTRAINT "explore_saved_queries_organization_id_fkey" FOREIGN KEY ("organization_id") REFERENCES "organization_metadata" ("id") ON UPDATE NO ACTION ON DELETE CASCADE,
  CONSTRAINT "explore_saved_queries_name_check" CHECK ((name <> ''::text) AND (char_length(name) <= 200))
);
-- Create index "explore_saved_queries_organization_id_updated_at_idx" to table: "explore_saved_queries"
CREATE INDEX "explore_saved_queries_organization_id_updated_at_idx" ON "explore_saved_queries" ("organization_id", "updated_at" DESC) WHERE (deleted IS FALSE);
