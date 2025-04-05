-- Modify "http_tool_definitions" table
ALTER TABLE "http_tool_definitions" ALTER COLUMN "server_env_var" DROP NOT NULL, ALTER COLUMN "security_type" DROP NOT NULL;
