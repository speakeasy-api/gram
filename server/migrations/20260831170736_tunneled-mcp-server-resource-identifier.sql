-- Modify "tunneled_mcp_servers" table
ALTER TABLE "tunneled_mcp_servers" ADD CONSTRAINT "tunneled_mcp_servers_resource_identifier_check" CHECK ((resource_identifier IS NULL) OR (resource_identifier <> ''::text)), ADD COLUMN "resource_identifier" text NULL;
-- Set comment to column: "resource_identifier" on table: "tunneled_mcp_servers"
COMMENT ON COLUMN "tunneled_mcp_servers"."resource_identifier" IS 'RFC 9728 protected-resource identifier of the tunneled server, recorded as the RFC 8707 resource on grants and used only for exact-match credential routing. Names a host inside the customer''s private network — never dialed by Gram.';
