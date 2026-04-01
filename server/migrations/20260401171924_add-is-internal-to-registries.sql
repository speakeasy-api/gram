-- Modify "mcp_registries" table
ALTER TABLE "mcp_registries" ADD COLUMN "is_internal" boolean NOT NULL DEFAULT false;
