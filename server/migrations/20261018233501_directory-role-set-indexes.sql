-- atlas:txmode none

-- Create index "directory_role_mappings_org_attribute_role_key" to table: "directory_role_mappings"
CREATE UNIQUE INDEX CONCURRENTLY "directory_role_mappings_org_attribute_role_key" ON "directory_role_mappings" ("organization_id", "attribute_key", "attribute_value", "role_urn") WHERE ((deleted IS FALSE) AND (attribute_key IS NOT NULL));
-- Create index "directory_role_mappings_org_group_role_key" to table: "directory_role_mappings"
CREATE UNIQUE INDEX CONCURRENTLY "directory_role_mappings_org_group_role_key" ON "directory_role_mappings" ("organization_id", "directory_group_id", "role_urn") WHERE ((deleted IS FALSE) AND (directory_group_id IS NOT NULL));
