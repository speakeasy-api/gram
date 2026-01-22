-- Modify "mcp_metadata" table
ALTER TABLE "mcp_metadata" ADD COLUMN "header_display_names" jsonb NOT NULL DEFAULT '{}';
