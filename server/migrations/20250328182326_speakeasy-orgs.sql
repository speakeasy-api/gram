-- Modify "deployment_statuses" table
ALTER TABLE "deployment_statuses" DROP CONSTRAINT "deployment_statuses_deployment_id_fkey";
-- Modify "deployments" table
ALTER TABLE "deployments" DROP CONSTRAINT "deployments_organization_id_fkey", DROP CONSTRAINT "deployments_project_id_fkey", DROP CONSTRAINT "deployments_user_id_fkey";
-- Modify "http_tool_definitions" table
ALTER TABLE "http_tool_definitions" DROP CONSTRAINT "http_tool_definitions_organization_id_fkey";
-- Modify "projects" table
ALTER TABLE "projects" DROP CONSTRAINT "projects_organization_id_fkey";
-- Drop "memberships" table
DROP TABLE "memberships";
-- Drop "organizations" table
DROP TABLE "organizations";
-- Drop "users" table
DROP TABLE "users";
