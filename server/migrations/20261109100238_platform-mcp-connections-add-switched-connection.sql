-- Modify "platform_mcp_connections" table
ALTER TABLE "platform_mcp_connections" ADD COLUMN "switched_connection_id" uuid NULL, ADD CONSTRAINT "platform_mcp_connections_switched_connection_id_fkey" FOREIGN KEY ("switched_connection_id") REFERENCES "platform_mcp_connections" ("id") ON UPDATE NO ACTION ON DELETE SET NULL;
-- Set comment to column: "switched_connection_id" on table: "platform_mcp_connections"
COMMENT ON COLUMN "platform_mcp_connections"."switched_connection_id" IS 'The connection, in another organization, that this connection''s tokens act through after the user switched organization in chat. Null means the tokens act in this connection''s own organization.
@access: confidential';
