-- atlas:txmode none

-- Create index "plugin_assignments_organization_id_principal_urn_idx" to table: "plugin_assignments"
CREATE INDEX CONCURRENTLY "plugin_assignments_organization_id_principal_urn_idx" ON "plugin_assignments" ("organization_id", "principal_urn");
