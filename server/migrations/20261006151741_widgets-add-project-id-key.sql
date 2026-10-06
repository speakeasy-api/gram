-- atlas:txmode none

-- Create index "widgets_project_id_id_key" to table: "widgets"
CREATE UNIQUE INDEX CONCURRENTLY "widgets_project_id_id_key" ON "widgets" ("project_id", "id");
