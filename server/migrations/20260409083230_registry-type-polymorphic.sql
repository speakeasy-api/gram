-- Replace polymorphic registry_type column with nullable foreign keys
-- Exactly one of registry_id or organization_mcp_collection_registry_id must be set

-- Drop old FK (was already dropped, but be safe with IF EXISTS)
ALTER TABLE "external_mcp_attachments" DROP CONSTRAINT IF EXISTS "external_mcp_attachments_registry_id_fkey";

-- Drop registry_type column and its check constraint
ALTER TABLE "external_mcp_attachments" DROP CONSTRAINT IF EXISTS "external_mcp_attachments_registry_type_check";
ALTER TABLE "external_mcp_attachments" DROP COLUMN IF EXISTS "registry_type";

-- Add nullable collection registry column
ALTER TABLE "external_mcp_attachments" ADD COLUMN "organization_mcp_collection_registry_id" uuid;

-- Make registry_id nullable (it was NOT NULL)
ALTER TABLE "external_mcp_attachments" ALTER COLUMN "registry_id" DROP NOT NULL;

-- Add proper foreign keys
ALTER TABLE "external_mcp_attachments"
  ADD CONSTRAINT "external_mcp_attachments_registry_id_fkey"
    FOREIGN KEY ("registry_id") REFERENCES "mcp_registries"("id") ON DELETE CASCADE,
  ADD CONSTRAINT "external_mcp_attachments_collection_registry_id_fkey"
    FOREIGN KEY ("organization_mcp_collection_registry_id") REFERENCES "organization_mcp_collection_registries"("id") ON DELETE CASCADE;

-- Enforce exactly one registry reference
ALTER TABLE "external_mcp_attachments"
  ADD CONSTRAINT "external_mcp_attachments_exactly_one_registry" CHECK (
    (registry_id IS NOT NULL)::int +
    (organization_mcp_collection_registry_id IS NOT NULL)::int = 1
  );
