-- atlas:txmode none

-- Create index "platform_mcp_catalog_registrations_org_connection_idx" to table: "platform_mcp_catalog_registrations"
CREATE INDEX CONCURRENTLY "platform_mcp_catalog_registrations_org_connection_idx" ON "platform_mcp_catalog_registrations" ("organization_id", "connection_id");
-- Create index "platform_mcp_catalog_registrations_organization_project_idx" to table: "platform_mcp_catalog_registrations"
CREATE INDEX CONCURRENTLY "platform_mcp_catalog_registrations_organization_project_idx" ON "platform_mcp_catalog_registrations" ("organization_id", "project_id");
-- Create index "platform_mcp_catalog_registrations_project_mcp_endpoint_idx" to table: "platform_mcp_catalog_registrations"
CREATE INDEX CONCURRENTLY "platform_mcp_catalog_registrations_project_mcp_endpoint_idx" ON "platform_mcp_catalog_registrations" ("project_id", "mcp_endpoint_id");
-- Create index "platform_mcp_catalog_registrations_project_mcp_server_idx" to table: "platform_mcp_catalog_registrations"
CREATE INDEX CONCURRENTLY "platform_mcp_catalog_registrations_project_mcp_server_idx" ON "platform_mcp_catalog_registrations" ("project_id", "mcp_server_id");
-- Create index "platform_mcp_catalog_registrations_project_remote_server_idx" to table: "platform_mcp_catalog_registrations"
CREATE INDEX CONCURRENTLY "platform_mcp_catalog_registrations_project_remote_server_idx" ON "platform_mcp_catalog_registrations" ("project_id", "remote_mcp_server_id");
-- Create index "platform_mcp_catalog_registrations_project_session_issuer_idx" to table: "platform_mcp_catalog_registrations"
CREATE INDEX CONCURRENTLY "platform_mcp_catalog_registrations_project_session_issuer_idx" ON "platform_mcp_catalog_registrations" ("project_id", "user_session_issuer_id");
-- Create index "platform_mcp_operation_receipts_organization_connection_idx" to table: "platform_mcp_operation_receipts"
CREATE INDEX CONCURRENTLY "platform_mcp_operation_receipts_organization_connection_idx" ON "platform_mcp_operation_receipts" ("organization_id", "connection_id");
-- Create index "platform_mcp_operation_receipts_project_registration_idx" to table: "platform_mcp_operation_receipts"
CREATE INDEX CONCURRENTLY "platform_mcp_operation_receipts_project_registration_idx" ON "platform_mcp_operation_receipts" ("project_id", "registration_id");
-- Create index "platform_mcp_readiness_organization_connection_idx" to table: "platform_mcp_readiness"
CREATE INDEX CONCURRENTLY "platform_mcp_readiness_organization_connection_idx" ON "platform_mcp_readiness" ("organization_id", "connection_id");
-- Create index "platform_mcp_readiness_organization_project_idx" to table: "platform_mcp_readiness"
CREATE INDEX CONCURRENTLY "platform_mcp_readiness_organization_project_idx" ON "platform_mcp_readiness" ("organization_id", "project_id");
-- Create index "platform_mcp_setup_handoffs_organization_connection_idx" to table: "platform_mcp_setup_handoffs"
CREATE INDEX CONCURRENTLY "platform_mcp_setup_handoffs_organization_connection_idx" ON "platform_mcp_setup_handoffs" ("organization_id", "connection_id");
-- Create index "platform_mcp_setup_handoffs_organization_project_idx" to table: "platform_mcp_setup_handoffs"
CREATE INDEX CONCURRENTLY "platform_mcp_setup_handoffs_organization_project_idx" ON "platform_mcp_setup_handoffs" ("organization_id", "project_id");
-- Create index "platform_mcp_setup_handoffs_project_registration_idx" to table: "platform_mcp_setup_handoffs"
CREATE INDEX CONCURRENTLY "platform_mcp_setup_handoffs_project_registration_idx" ON "platform_mcp_setup_handoffs" ("project_id", "registration_id");
