-- Modify "http_tool_definitions" table
ALTER TABLE "http_tool_definitions" ALTER COLUMN "server_env_var" SET NOT NULL, ADD COLUMN "default_server_url" text NULL;
