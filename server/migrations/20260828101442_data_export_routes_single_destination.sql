-- atlas:txmode none

-- Drop index "data_export_routes_project_source_destination_key" from table: "data_export_routes"
DROP INDEX CONCURRENTLY "data_export_routes_project_source_destination_key";
-- Create index "data_export_routes_project_source_key" to table: "data_export_routes"
CREATE UNIQUE INDEX CONCURRENTLY "data_export_routes_project_source_key" ON "data_export_routes" ("project_id", "data_source") WHERE (deleted IS FALSE);
