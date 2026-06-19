-- atlas:txmode none

-- Create index "trigger_instances_dashboard_target_uniq" to table: "trigger_instances"
CREATE UNIQUE INDEX CONCURRENTLY "trigger_instances_dashboard_target_uniq" ON "trigger_instances" ("project_id", "target_ref") WHERE ((definition_slug = 'dashboard'::text) AND (status = 'active'::text) AND (deleted IS FALSE));
