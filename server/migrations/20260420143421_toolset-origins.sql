-- Create "toolset_origins" table
CREATE TABLE "toolset_origins" (
  "id" uuid NOT NULL DEFAULT generate_uuidv7(),
  "organization_id" text NOT NULL,
  "toolset_id" uuid NOT NULL,
  "origin_registry_specifier" text NOT NULL,
  "created_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "updated_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "deleted_at" timestamptz NULL,
  "deleted" boolean NOT NULL GENERATED ALWAYS AS (deleted_at IS NOT NULL) STORED,
  PRIMARY KEY ("id"),
  CONSTRAINT "toolset_origins_organization_id_fkey" FOREIGN KEY ("organization_id") REFERENCES "organization_metadata" ("id") ON UPDATE NO ACTION ON DELETE CASCADE,
  CONSTRAINT "toolset_origins_toolset_id_fkey" FOREIGN KEY ("toolset_id") REFERENCES "toolsets" ("id") ON UPDATE NO ACTION ON DELETE CASCADE,
  CONSTRAINT "toolset_origins_origin_registry_specifier_check" CHECK (origin_registry_specifier <> ''::text)
);
-- Create index "toolset_origins_origin_registry_specifier_idx" to table: "toolset_origins"
CREATE INDEX "toolset_origins_origin_registry_specifier_idx" ON "toolset_origins" ("origin_registry_specifier") WHERE (deleted IS FALSE);
-- Create index "toolset_origins_toolset_id_key" to table: "toolset_origins"
CREATE UNIQUE INDEX "toolset_origins_toolset_id_key" ON "toolset_origins" ("toolset_id") WHERE (deleted IS FALSE);
