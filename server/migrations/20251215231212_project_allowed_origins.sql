-- atlas:txmode none

-- Create index "project_allowed_origins_project_id_origin_key" to table: "project_allowed_origins"
CREATE UNIQUE INDEX CONCURRENTLY "project_allowed_origins_project_id_origin_key" ON "project_allowed_origins" ("project_id", "origin") WHERE (deleted IS FALSE);
