-- Modify "deployments" table
ALTER TABLE "deployments" ALTER COLUMN "user_id" TYPE text, ALTER COLUMN "organization_id" TYPE text;
-- Modify "http_tool_definitions" table
ALTER TABLE "http_tool_definitions" ALTER COLUMN "organization_id" TYPE text;
-- Modify "projects" table
ALTER TABLE "projects" ALTER COLUMN "organization_id" TYPE text;
-- Modify "toolsets" table
ALTER TABLE "toolsets" ALTER COLUMN "organization_id" TYPE text;
