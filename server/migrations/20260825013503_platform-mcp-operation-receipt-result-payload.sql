-- Modify "platform_mcp_operation_receipts" table
ALTER TABLE "platform_mcp_operation_receipts" ADD COLUMN "result_payload" jsonb NULL;
