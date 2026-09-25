-- atlas:txmode none

-- Create index "directory_groups_organization_id_id_key" to table: "directory_groups"
CREATE UNIQUE INDEX CONCURRENTLY "directory_groups_organization_id_id_key" ON "directory_groups" ("organization_id", "id");
-- Create "directory_role_mappings" table
CREATE TABLE "directory_role_mappings" (
  "id" uuid NOT NULL DEFAULT generate_uuidv7(),
  "organization_id" text NOT NULL,
  "source_kind" text NOT NULL,
  "directory_group_id" uuid NULL,
  "attribute_key" text NULL,
  "attribute_value" text NULL,
  "role_urn" text NOT NULL,
  "created_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "updated_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "deleted_at" timestamptz NULL,
  "deleted" boolean NOT NULL GENERATED ALWAYS AS (deleted_at IS NOT NULL) STORED,
  PRIMARY KEY ("id"),
  CONSTRAINT "directory_role_mappings_directory_group_fkey" FOREIGN KEY ("organization_id", "directory_group_id") REFERENCES "directory_groups" ("organization_id", "id") ON UPDATE NO ACTION ON DELETE CASCADE,
  CONSTRAINT "directory_role_mappings_organization_id_fkey" FOREIGN KEY ("organization_id") REFERENCES "organization_metadata" ("id") ON UPDATE NO ACTION ON DELETE CASCADE,
  CONSTRAINT "directory_role_mappings_source_columns_check" CHECK (((attribute_key IS NULL) = (attribute_value IS NULL)) AND (NOT ((directory_group_id IS NOT NULL) AND (attribute_key IS NOT NULL))))
);
-- Create index "directory_role_mappings_org_attribute_key" to table: "directory_role_mappings"
CREATE UNIQUE INDEX "directory_role_mappings_org_attribute_key" ON "directory_role_mappings" ("organization_id", "attribute_key", "attribute_value") WHERE ((deleted IS FALSE) AND (attribute_key IS NOT NULL));
-- Create index "directory_role_mappings_org_group_key" to table: "directory_role_mappings"
CREATE UNIQUE INDEX "directory_role_mappings_org_group_key" ON "directory_role_mappings" ("organization_id", "directory_group_id") WHERE ((deleted IS FALSE) AND (directory_group_id IS NOT NULL));
