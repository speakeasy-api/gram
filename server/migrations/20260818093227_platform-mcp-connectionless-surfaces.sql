-- atlas:txmode none

-- Modify "platform_mcp_distributions" table
ALTER TABLE "platform_mcp_distributions" ADD CONSTRAINT "platform_mcp_distributions_connection_generation_check" CHECK ((connection_id IS NULL) = (connection_generation IS NULL)) NOT VALID, ALTER COLUMN "connection_id" DROP NOT NULL, ALTER COLUMN "connection_generation" DROP NOT NULL, ADD COLUMN "user_id" text NULL, ADD COLUMN "acting_surface" text NULL;
ALTER TABLE "platform_mcp_distributions" VALIDATE CONSTRAINT "platform_mcp_distributions_connection_generation_check";
-- Modify "platform_mcp_readiness" table
ALTER TABLE "platform_mcp_readiness" ADD CONSTRAINT "platform_mcp_readiness_connection_generation_check" CHECK ((connection_id IS NULL) = (connection_generation IS NULL)) NOT VALID, ALTER COLUMN "connection_id" DROP NOT NULL, ALTER COLUMN "connection_generation" DROP NOT NULL, ADD COLUMN "user_id" text NULL, ADD COLUMN "acting_surface" text NULL;
ALTER TABLE "platform_mcp_readiness" VALIDATE CONSTRAINT "platform_mcp_readiness_connection_generation_check";
-- Create index "platform_mcp_readiness_binding_key" to table: "platform_mcp_readiness"
-- Built under a temporary name and swapped in, rather than dropped and recreated:
-- a drop-then-create leaves a window with no unique arbiter, in which the
-- readiness upsert's ON CONFLICT has no index to infer and duplicate bindings
-- can be written.
CREATE UNIQUE INDEX CONCURRENTLY "platform_mcp_readiness_binding_key_swap" ON "platform_mcp_readiness" ("registration_id", "connection_id", "connection_generation", "provider_authorization_fingerprint") NULLS NOT DISTINCT;
DROP INDEX CONCURRENTLY "platform_mcp_readiness_binding_key";
ALTER INDEX "platform_mcp_readiness_binding_key_swap" RENAME TO "platform_mcp_readiness_binding_key";
-- Modify "platform_mcp_setup_handoffs" table
ALTER TABLE "platform_mcp_setup_handoffs" ADD CONSTRAINT "platform_mcp_setup_handoffs_connection_generation_check" CHECK ((connection_id IS NULL) = (connection_generation IS NULL)) NOT VALID, ALTER COLUMN "connection_id" DROP NOT NULL, ALTER COLUMN "connection_generation" DROP NOT NULL, ADD COLUMN "user_id" text NULL, ADD COLUMN "acting_surface" text NULL;
ALTER TABLE "platform_mcp_setup_handoffs" VALIDATE CONSTRAINT "platform_mcp_setup_handoffs_connection_generation_check";
-- Create index "platform_mcp_setup_handoffs_active_binding_key" to table: "platform_mcp_setup_handoffs"
-- Swapped in the same way, so the one-active-handoff invariant is never
-- unenforced while the migration runs.
CREATE UNIQUE INDEX CONCURRENTLY "platform_mcp_setup_handoffs_active_binding_key_swap" ON "platform_mcp_setup_handoffs" ("registration_id", "connection_id", "connection_generation", "intent") NULLS NOT DISTINCT WHERE ((redeemed_at IS NULL) AND (invalidated_at IS NULL));
DROP INDEX CONCURRENTLY "platform_mcp_setup_handoffs_active_binding_key";
ALTER INDEX "platform_mcp_setup_handoffs_active_binding_key_swap" RENAME TO "platform_mcp_setup_handoffs_active_binding_key";
