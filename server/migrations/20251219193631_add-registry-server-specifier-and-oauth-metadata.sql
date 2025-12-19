-- Modify "external_mcp_attachments" table
ALTER TABLE "external_mcp_attachments" ADD CONSTRAINT "external_mcp_attachments_registry_server_specifier_check" CHECK (registry_server_specifier <> ''::text), ADD COLUMN "registry_server_specifier" text NOT NULL;
-- Modify "external_mcp_tool_definitions" table
ALTER TABLE "external_mcp_tool_definitions" ADD COLUMN "oauth_version" text NOT NULL DEFAULT 'none', ADD COLUMN "oauth_authorization_endpoint" text NULL, ADD COLUMN "oauth_token_endpoint" text NULL, ADD COLUMN "oauth_registration_endpoint" text NULL, ADD COLUMN "oauth_scopes_supported" text[] NULL;
