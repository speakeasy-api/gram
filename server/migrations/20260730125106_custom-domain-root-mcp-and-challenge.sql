-- atlas:txmode none

-- Modify "custom_domains" table
ALTER TABLE "custom_domains" ADD CONSTRAINT "custom_domains_openai_apps_challenge_token_check" CHECK ((openai_apps_challenge_token IS NULL) OR ((openai_apps_challenge_token <> ''::text) AND (char_length(openai_apps_challenge_token) <= 256) AND (POSITION((chr(10)) IN (openai_apps_challenge_token)) = 0) AND (POSITION((chr(13)) IN (openai_apps_challenge_token)) = 0))), ADD COLUMN "openai_apps_challenge_token" text NULL;
-- Modify "mcp_endpoints" table
ALTER TABLE "mcp_endpoints" ADD CONSTRAINT "mcp_endpoints_domain_root_requires_custom_domain_check" CHECK ((is_domain_root IS NOT TRUE) OR (custom_domain_id IS NOT NULL)), ADD COLUMN "is_domain_root" boolean NULL;
-- Create index "mcp_endpoints_custom_domain_id_root_key" to table: "mcp_endpoints"
CREATE UNIQUE INDEX CONCURRENTLY "mcp_endpoints_custom_domain_id_root_key" ON "mcp_endpoints" ("custom_domain_id") WHERE ((is_domain_root IS TRUE) AND (deleted IS FALSE));
