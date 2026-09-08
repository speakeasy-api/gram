-- atlas:txmode none

-- Create index "agents_owner_all_idx" to table: "agents"
CREATE INDEX CONCURRENTLY "agents_owner_all_idx" ON "agents" ("owner_user_id");
-- Create index "organization_user_relationships_user_org_active_idx" to table: "organization_user_relationships"
CREATE INDEX CONCURRENTLY "organization_user_relationships_user_org_active_idx" ON "organization_user_relationships" ("user_id", "organization_id") WHERE (deleted IS FALSE);
