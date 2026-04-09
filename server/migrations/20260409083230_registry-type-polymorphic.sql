-- Modify "external_mcp_attachments" table
ALTER TABLE "external_mcp_attachments" DROP CONSTRAINT "external_mcp_attachments_registry_id_fkey", ADD CONSTRAINT "external_mcp_attachments_registry_type_check" CHECK (registry_type = ANY (ARRAY['external'::text, 'internal'::text])), ADD COLUMN "registry_type" text NOT NULL DEFAULT 'external';
