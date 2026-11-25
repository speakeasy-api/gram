-- atlas:txmode none

-- Drop index "directory_role_mappings_org_attribute_key" from table: "directory_role_mappings"
DROP INDEX CONCURRENTLY "directory_role_mappings_org_attribute_key";
-- Drop index "directory_role_mappings_org_group_key" from table: "directory_role_mappings"
DROP INDEX CONCURRENTLY "directory_role_mappings_org_group_key";
