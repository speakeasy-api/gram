-- Modify "http_tool_definitions" table
ALTER TABLE "http_tool_definitions" DROP COLUMN "headers_schema", DROP COLUMN "queries_schema", DROP COLUMN "pathparams_schema", DROP COLUMN "body_schema", ADD COLUMN "schema_version" text NOT NULL, ADD COLUMN "schema" jsonb NULL;
