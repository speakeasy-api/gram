-- Modify "http_tool_definitions" table
ALTER TABLE "http_tool_definitions" DROP CONSTRAINT "http_tool_definitions_security_type_check", DROP COLUMN "security_type", DROP COLUMN "bearer_env_var", DROP COLUMN "apikey_env_var", DROP COLUMN "username_env_var", DROP COLUMN "password_env_var", ADD COLUMN "security" jsonb NULL;
