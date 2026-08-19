-- atlas:txmode none

-- Modify "mcp_registries" table
ALTER TABLE "mcp_registries" ADD COLUMN "source_type" text NULL, ADD COLUMN "auth_profile" text NULL, ADD COLUMN "enabled" boolean NULL, ADD COLUMN "certification_state" text NULL, ADD COLUMN "certification_version" text NULL, ADD COLUMN "priority" integer NULL, ADD COLUMN "source_key" text NULL;
-- Create index "mcp_registries_source_key_key" to table: "mcp_registries"
CREATE UNIQUE INDEX CONCURRENTLY "mcp_registries_source_key_key" ON "mcp_registries" ("source_key") WHERE ((source_key IS NOT NULL) AND (deleted IS FALSE));
-- Modify "platform_mcp_readiness" table
ALTER TABLE "platform_mcp_readiness" ADD CONSTRAINT "platform_mcp_readiness_connectionless_actor_check" CHECK ((connection_id IS NOT NULL) OR ((user_id IS NOT NULL) AND (acting_surface IS NOT NULL)));
-- Create index "platform_mcp_readiness_assistant_binding_key" to table: "platform_mcp_readiness"
CREATE UNIQUE INDEX CONCURRENTLY "platform_mcp_readiness_assistant_binding_key" ON "platform_mcp_readiness" ("registration_id", "user_id", "acting_surface", "provider_authorization_fingerprint") WHERE (connection_id IS NULL);
-- Create index "platform_mcp_readiness_external_binding_key" to table: "platform_mcp_readiness"
CREATE UNIQUE INDEX CONCURRENTLY "platform_mcp_readiness_external_binding_key" ON "platform_mcp_readiness" ("registration_id", "connection_id", "connection_generation", "provider_authorization_fingerprint") WHERE (connection_id IS NOT NULL);
-- Create index "platform_mcp_readiness_organization_actor_idx" to table: "platform_mcp_readiness"
CREATE INDEX CONCURRENTLY "platform_mcp_readiness_organization_actor_idx" ON "platform_mcp_readiness" ("organization_id", "user_id", "acting_surface") WHERE (connection_id IS NULL);
-- Modify "platform_mcp_distributions" table
ALTER TABLE "platform_mcp_distributions" ADD CONSTRAINT "platform_mcp_distributions_connectionless_actor_check" CHECK ((connection_id IS NOT NULL) OR ((user_id IS NOT NULL) AND (acting_surface IS NOT NULL))), ADD COLUMN "plugin_id" uuid NULL, ADD CONSTRAINT "platform_mcp_distributions_project_plugin_fkey" FOREIGN KEY ("project_id", "plugin_id") REFERENCES "plugins" ("project_id", "id") ON UPDATE NO ACTION ON DELETE CASCADE;
-- Create index "platform_mcp_distributions_organization_actor_idx" to table: "platform_mcp_distributions"
CREATE INDEX CONCURRENTLY "platform_mcp_distributions_organization_actor_idx" ON "platform_mcp_distributions" ("organization_id", "user_id", "acting_surface") WHERE (connection_id IS NULL);
-- Create index "platform_mcp_distributions_plugin_identity_key" to table: "platform_mcp_distributions"
CREATE UNIQUE INDEX CONCURRENTLY "platform_mcp_distributions_plugin_identity_key" ON "platform_mcp_distributions" ("organization_id", "project_id", "registration_id", "plugin_id") WHERE (plugin_id IS NOT NULL);
-- Create index "platform_mcp_distributions_project_plugin_idx" to table: "platform_mcp_distributions"
CREATE INDEX CONCURRENTLY "platform_mcp_distributions_project_plugin_idx" ON "platform_mcp_distributions" ("project_id", "plugin_id") WHERE (plugin_id IS NOT NULL);
