-- Create "roles" table
CREATE TABLE "roles" (
  "id" uuid NOT NULL DEFAULT generate_uuidv7(),
  "organization_id" text NOT NULL,
  "workos_id" text NOT NULL,
  "workos_slug" text NOT NULL,
  "workos_name" text NOT NULL,
  "workos_description" text NULL,
  "workos_created_at" timestamptz NOT NULL,
  "workos_updated_at" timestamptz NOT NULL,
  "created_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "updated_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "deleted_at" timestamptz NULL,
  "deleted" boolean NOT NULL GENERATED ALWAYS AS (deleted_at IS NOT NULL) STORED,
  PRIMARY KEY ("id"),
  CONSTRAINT "roles_organization_id_fkey" FOREIGN KEY ("organization_id") REFERENCES "organization_metadata" ("id") ON UPDATE NO ACTION ON DELETE CASCADE
);
-- Create index "roles_organization_id_workos_id_key" to table: "roles"
CREATE UNIQUE INDEX "roles_organization_id_workos_id_key" ON "roles" ("organization_id", "workos_id") WHERE (deleted IS FALSE);
