-- Modify "deployment_logs" table
ALTER TABLE "deployment_logs" ALTER COLUMN "deployment_id" SET NOT NULL, ALTER COLUMN "project_id" SET NOT NULL;
-- Modify "deployment_statuses" table
ALTER TABLE "deployment_statuses" DROP CONSTRAINT "deployment_statuses_deployment_id_fkey", ALTER COLUMN "deployment_id" SET NOT NULL;
-- Modify "deployments" table
ALTER TABLE "deployments" DROP CONSTRAINT "deployments_organization_id_fkey", DROP CONSTRAINT "deployments_project_id_fkey", DROP CONSTRAINT "deployments_user_id_fkey", ALTER COLUMN "user_id" TYPE character varying, ALTER COLUMN "project_id" SET NOT NULL, ALTER COLUMN "organization_id" SET NOT NULL;
-- Modify "http_tool_definitions" table
ALTER TABLE "http_tool_definitions" DROP CONSTRAINT "http_tool_definitions_organization_id_fkey", ALTER COLUMN "organization_id" SET NOT NULL, ALTER COLUMN "project_id" SET NOT NULL;
-- Modify "projects" table
ALTER TABLE "projects" DROP CONSTRAINT "projects_organization_id_fkey", ALTER COLUMN "organization_id" SET NOT NULL;
-- Drop "memberships" table
DROP TABLE "memberships";
-- Drop "organizations" table
DROP TABLE "organizations";
-- Drop "users" table
DROP TABLE "users";
