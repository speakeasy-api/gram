-- Modify "api_keys" table
ALTER TABLE "api_keys" ALTER COLUMN "name" TYPE character varying(40);
-- Modify "assets" table
ALTER TABLE "assets" ALTER COLUMN "name" TYPE character varying(100);
-- Modify "deployments" table
ALTER TABLE "deployments" ALTER COLUMN "github_repo" TYPE character varying(100), ALTER COLUMN "github_pr" TYPE character varying(100), ALTER COLUMN "external_id" TYPE character varying(100), ALTER COLUMN "external_url" TYPE character varying(100), ALTER COLUMN "github_sha" TYPE character varying(100);
-- Modify "deployments_openapiv3_assets" table
ALTER TABLE "deployments_openapiv3_assets" ALTER COLUMN "name" TYPE character varying(60), ALTER COLUMN "slug" TYPE character varying(60);
-- Modify "environment_entries" table
ALTER TABLE "environment_entries" ALTER COLUMN "name" TYPE character varying(60), ALTER COLUMN "value" TYPE character varying(4000);
-- Modify "environments" table
ALTER TABLE "environments" ALTER COLUMN "name" TYPE character varying(40), ALTER COLUMN "slug" TYPE character varying(40), ALTER COLUMN "description" TYPE character varying(100);
-- Modify "http_security" table
ALTER TABLE "http_security" ALTER COLUMN "key" TYPE character varying(60), ALTER COLUMN "type" TYPE character varying(20), ALTER COLUMN "type" DROP NOT NULL, ALTER COLUMN "name" TYPE character varying(60), ALTER COLUMN "in_placement" TYPE character varying(10), ALTER COLUMN "scheme" TYPE character varying(20), ALTER COLUMN "bearer_format" TYPE character varying(20), ALTER COLUMN "env_variables" TYPE character varying(60)[];
-- Modify "http_tool_definitions" table
ALTER TABLE "http_tool_definitions" ALTER COLUMN "name" TYPE character varying(100), ALTER COLUMN "http_method" TYPE character varying(20), ALTER COLUMN "path" TYPE character varying(140), ALTER COLUMN "tags" TYPE character varying(40)[], ALTER COLUMN "openapiv3_operation" TYPE character varying(100), ALTER COLUMN "schema_version" TYPE character varying(20);
-- Modify "projects" table
ALTER TABLE "projects" ALTER COLUMN "name" TYPE character varying(40), ALTER COLUMN "slug" TYPE character varying(40);
-- Modify "toolsets" table
ALTER TABLE "toolsets" ALTER COLUMN "name" TYPE character varying(40), ALTER COLUMN "description" TYPE character varying(100), ALTER COLUMN "slug" TYPE character varying(40), ALTER COLUMN "http_tool_names" TYPE character varying(100)[], ALTER COLUMN "default_environment_slug" TYPE character varying(40);
