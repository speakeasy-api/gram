-- atlas:txmode none

-- Create index "platform_mcp_operation_receipts_user_operation_idx" to table: "platform_mcp_operation_receipts"
CREATE INDEX CONCURRENTLY "platform_mcp_operation_receipts_user_operation_idx" ON "platform_mcp_operation_receipts" ("organization_id", "user_id", "operation", "idempotency_key");
