-- Create "private_catalog_servers" table
CREATE TABLE "private_catalog_servers" (
  "id" uuid NOT NULL DEFAULT generate_uuidv7(),
  "organization_id" text NOT NULL,
  "project_id" uuid NULL,
  "published_by" text NOT NULL,
  "name" text NOT NULL,
  "slug" text NOT NULL,
  "description" text NULL,
  "created_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "updated_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "deleted_at" timestamptz NULL,
  "deleted" boolean NOT NULL GENERATED ALWAYS AS (deleted_at IS NOT NULL) STORED,
  PRIMARY KEY ("id"),
  CONSTRAINT "private_catalog_servers_project_id_fkey" FOREIGN KEY ("project_id") REFERENCES "projects" ("id") ON UPDATE NO ACTION ON DELETE SET NULL,
  CONSTRAINT "private_catalog_servers_name_check" CHECK ((name <> ''::text) AND (char_length(name) <= 100)),
  CONSTRAINT "private_catalog_servers_slug_check" CHECK ((slug <> ''::text) AND (char_length(slug) <= 100))
);
-- Create index "private_catalog_servers_organization_id_slug_key" to table: "private_catalog_servers"
CREATE UNIQUE INDEX "private_catalog_servers_organization_id_slug_key" ON "private_catalog_servers" ("organization_id", "slug") WHERE (deleted IS FALSE);
