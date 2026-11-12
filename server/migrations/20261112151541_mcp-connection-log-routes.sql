-- atlas:txmode none

-- Create index "data_export_routes_tenant_id_source_key" to table: "data_export_routes"
CREATE UNIQUE INDEX CONCURRENTLY "data_export_routes_tenant_id_source_key" ON "data_export_routes" ("organization_id", "project_id", "id", "data_source");
-- Create "mcp_connection_log_routes" table
CREATE TABLE "mcp_connection_log_routes" (
  "data_export_route_id" uuid NOT NULL,
  "data_export_routes_data_source" text NOT NULL DEFAULT 'mcp_connection_logs',
  "organization_id" text NOT NULL,
  "project_id" uuid NOT NULL,
  "min_severity" text NOT NULL DEFAULT 'error',
  "created_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "updated_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  PRIMARY KEY ("data_export_route_id"),
  CONSTRAINT "mcp_connection_log_routes_fkey" FOREIGN KEY ("organization_id", "project_id", "data_export_route_id", "data_export_routes_data_source") REFERENCES "data_export_routes" ("organization_id", "project_id", "id", "data_source") ON UPDATE NO ACTION ON DELETE CASCADE,
  CONSTRAINT "mcp_connection_log_routes_data_source_check" CHECK (data_export_routes_data_source = 'mcp_connection_logs'::text)
);
-- Set comment to column: "data_export_route_id" on table: "mcp_connection_log_routes"
COMMENT ON COLUMN "mcp_connection_log_routes"."data_export_route_id" IS '@access: confidential';
-- Set comment to column: "data_export_routes_data_source" on table: "mcp_connection_log_routes"
COMMENT ON COLUMN "mcp_connection_log_routes"."data_export_routes_data_source" IS '@access: confidential';
-- Set comment to column: "organization_id" on table: "mcp_connection_log_routes"
COMMENT ON COLUMN "mcp_connection_log_routes"."organization_id" IS '@access: confidential';
-- Set comment to column: "project_id" on table: "mcp_connection_log_routes"
COMMENT ON COLUMN "mcp_connection_log_routes"."project_id" IS '@access: confidential';
-- Set comment to column: "min_severity" on table: "mcp_connection_log_routes"
COMMENT ON COLUMN "mcp_connection_log_routes"."min_severity" IS 'Lowest log severity this route sends: info, warn or error.
@access: confidential';
-- Set comment to column: "created_at" on table: "mcp_connection_log_routes"
COMMENT ON COLUMN "mcp_connection_log_routes"."created_at" IS '@access: confidential';
-- Set comment to column: "updated_at" on table: "mcp_connection_log_routes"
COMMENT ON COLUMN "mcp_connection_log_routes"."updated_at" IS '@access: confidential';
