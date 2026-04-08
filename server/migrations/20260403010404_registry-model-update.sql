-- atlas:txmode none

-- Create "organization_mcp_collections" table
CREATE TABLE "organization_mcp_collections" (
  "id" uuid NOT NULL DEFAULT generate_uuidv7(),
  "registry_id" uuid NOT NULL,
  "organization_id" text NOT NULL,
  "name" text NOT NULL CHECK (name <> '' AND CHAR_LENGTH(name) <= 100),
  "description" text NULL,
  "slug" text NOT NULL,
  "mcp_registry_namespace" text NOT NULL,
  "visibility" text NOT NULL,
  "created_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "updated_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "deleted_at" timestamptz NULL,
  "deleted" boolean NOT NULL GENERATED ALWAYS AS (deleted_at IS NOT NULL) STORED,
  PRIMARY KEY ("id"),
  CONSTRAINT "organization_mcp_collections_registry_id_fkey" FOREIGN KEY ("registry_id") REFERENCES "mcp_registries" ("id") ON UPDATE NO ACTION ON DELETE CASCADE,
  CONSTRAINT "organization_mcp_collections_organization_id_fkey" FOREIGN KEY ("organization_id") REFERENCES "organization_metadata" ("id") ON UPDATE NO ACTION ON DELETE CASCADE
);
-- Create "mcp_registry_toolsets" table
CREATE TABLE "mcp_registry_toolsets" (
  "published_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "created_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "updated_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "deleted_at" timestamptz NULL,
  "published_by" text NULL,
  "id" uuid NOT NULL DEFAULT generate_uuidv7(),
  "collection_id" uuid NOT NULL,
  "toolset_id" uuid NOT NULL,
  "deleted" boolean NOT NULL GENERATED ALWAYS AS (deleted_at IS NOT NULL) STORED,
  PRIMARY KEY ("id"),
  CONSTRAINT "mcp_registry_toolsets_collection_id_fkey" FOREIGN KEY ("collection_id") REFERENCES "organization_mcp_collections" ("id") ON UPDATE NO ACTION ON DELETE CASCADE,
  CONSTRAINT "mcp_registry_toolsets_toolset_id_fkey" FOREIGN KEY ("toolset_id") REFERENCES "toolsets" ("id") ON UPDATE NO ACTION ON DELETE CASCADE
);
