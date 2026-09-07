-- atlas:txmode none

-- Drop index "platform_mcp_onboarding_milestones_attempt_target_key" from table: "platform_mcp_onboarding_milestones"
DROP INDEX CONCURRENTLY "platform_mcp_onboarding_milestones_attempt_target_key";
-- Create index "platform_mcp_onboarding_milestones_attempt_target_key" to table: "platform_mcp_onboarding_milestones"
CREATE UNIQUE INDEX CONCURRENTLY "platform_mcp_onboarding_milestones_attempt_target_key" ON "platform_mcp_onboarding_milestones" ("organization_id", "milestone", "project_id", "mcp_key", "attempt_id", "connection_generation") NULLS NOT DISTINCT WHERE (attempt_id IS NOT NULL);
