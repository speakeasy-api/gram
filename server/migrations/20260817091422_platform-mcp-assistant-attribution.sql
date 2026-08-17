-- atlas:txmode none

-- Modify "platform_mcp_catalog_registrations" table
ALTER TABLE "platform_mcp_catalog_registrations" ADD CONSTRAINT "platform_mcp_catalog_registrations_connection_pair_check" CHECK ((connection_id IS NULL) = (connection_generation IS NULL)), ALTER COLUMN "connection_id" DROP NOT NULL, ALTER COLUMN "connection_generation" DROP NOT NULL, ADD COLUMN "user_id" text NULL, ADD COLUMN "acting_surface" text NULL;
-- Modify "platform_mcp_operation_receipts" table
ALTER TABLE "platform_mcp_operation_receipts" ADD CONSTRAINT "platform_mcp_operation_receipts_connection_pair_check" CHECK ((connection_id IS NULL) = (connection_generation IS NULL)), ALTER COLUMN "connection_id" DROP NOT NULL, ALTER COLUMN "connection_generation" DROP NOT NULL, ADD COLUMN "user_id" text NULL, ADD COLUMN "acting_surface" text NULL;
-- Create index "platform_mcp_operation_receipts_user_operation_key" to table: "platform_mcp_operation_receipts"
CREATE UNIQUE INDEX CONCURRENTLY "platform_mcp_operation_receipts_user_operation_key" ON "platform_mcp_operation_receipts" ("organization_id", "user_id", "project_id", "operation", "idempotency_key") WHERE (user_id IS NOT NULL);
