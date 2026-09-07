-- atlas:txmode none

-- Drop index "data_export_routes_project_source_key" from table: "data_export_routes"
DROP INDEX CONCURRENTLY "data_export_routes_project_source_key";
-- Create index "data_export_routes_project_source_destination_key" to table: "data_export_routes"
CREATE UNIQUE INDEX CONCURRENTLY "data_export_routes_project_source_destination_key" ON "data_export_routes" ("project_id", "data_source", "otel_destination_id") WHERE (deleted IS FALSE);
-- Modify "otel_destinations" table
ALTER TABLE "otel_destinations" ADD COLUMN "name" text NOT NULL;
