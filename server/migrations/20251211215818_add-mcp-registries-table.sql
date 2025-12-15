-- Create "mcp_registries" table
CREATE TABLE "mcp_registries" (
  "id" uuid NOT NULL DEFAULT generate_uuidv7(),
  "name" text NOT NULL,
  "url" text NOT NULL,
  "created_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "updated_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "deleted_at" timestamptz NULL,
  "deleted" boolean NOT NULL GENERATED ALWAYS AS (deleted_at IS NOT NULL) STORED,
  PRIMARY KEY ("id"),
  CONSTRAINT "mcp_registries_name_check" CHECK ((name <> ''::text) AND (char_length(name) <= 100)),
  CONSTRAINT "mcp_registries_url_check" CHECK ((url <> ''::text) AND (char_length(url) <= 500))
);
-- Create index "mcp_registries_url_key" to table: "mcp_registries"
CREATE UNIQUE INDEX "mcp_registries_url_key" ON "mcp_registries" ("url") WHERE (deleted IS FALSE);
