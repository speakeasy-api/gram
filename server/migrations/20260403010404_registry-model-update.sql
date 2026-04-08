-- atlas:txmode none

-- Create "organization_mcp_collections" table
CREATE TABLE "organization_mcp_collections" (
  "id" uuid NOT NULL DEFAULT generate_uuidv7(),
  "organization_id" text NOT NULL,
  "name" text NOT NULL CHECK (name <> '' AND CHAR_LENGTH(name) <= 100),
  "description" text NULL,
  "slug" text NOT NULL,
  "visibility" text NOT NULL,
  "created_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "updated_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "deleted_at" timestamptz NULL,
  "deleted" boolean NOT NULL GENERATED ALWAYS AS (deleted_at IS NOT NULL) STORED,
  PRIMARY KEY ("id"),
  CONSTRAINT "organization_mcp_collections_organization_id_fkey" FOREIGN KEY ("organization_id") REFERENCES "organization_metadata" ("id") ON UPDATE NO ACTION ON DELETE CASCADE
);
-- Create "organization_mcp_collection_registries" table
CREATE TABLE "organization_mcp_collection_registries" (
  "id" uuid NOT NULL DEFAULT generate_uuidv7(),
  "collection_id" uuid NOT NULL,
  "registry_id" uuid NOT NULL,
  "namespace" text NOT NULL,
  "created_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "updated_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "deleted_at" timestamptz NULL,
  "deleted" boolean NOT NULL GENERATED ALWAYS AS (deleted_at IS NOT NULL) STORED,
  PRIMARY KEY ("id"),
  CONSTRAINT "organization_mcp_collection_registries_collection_id_fkey" FOREIGN KEY ("collection_id") REFERENCES "organization_mcp_collections" ("id") ON UPDATE NO ACTION ON DELETE CASCADE,
  CONSTRAINT "organization_mcp_collection_registries_registry_id_fkey" FOREIGN KEY ("registry_id") REFERENCES "mcp_registries" ("id") ON UPDATE NO ACTION ON DELETE CASCADE
);
-- Create "organization_mcp_collection_server_attachments" table
CREATE TABLE "organization_mcp_collection_server_attachments" (
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
  CONSTRAINT "organization_mcp_collection_server_attachments_collection_id_fkey" FOREIGN KEY ("collection_id") REFERENCES "organization_mcp_collections" ("id") ON UPDATE NO ACTION ON DELETE CASCADE,
  CONSTRAINT "organization_mcp_collection_server_attachments_toolset_id_fkey" FOREIGN KEY ("toolset_id") REFERENCES "toolsets" ("id") ON UPDATE NO ACTION ON DELETE CASCADE
);
