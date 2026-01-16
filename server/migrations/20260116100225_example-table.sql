-- Create "example_table" table
CREATE TABLE "example_table" (
  "id" uuid NOT NULL DEFAULT generate_uuidv7(),
  "name" text NOT NULL,
  "description" text NULL,
  "created_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "updated_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "deleted_at" timestamptz NULL,
  "deleted" boolean NOT NULL GENERATED ALWAYS AS (deleted_at IS NOT NULL) STORED,
  PRIMARY KEY ("id"),
  CONSTRAINT "example_table_description_check" CHECK ((description <> ''::text) AND (char_length(description) <= 500)),
  CONSTRAINT "example_table_name_check" CHECK ((name <> ''::text) AND (char_length(name) <= 100))
);
