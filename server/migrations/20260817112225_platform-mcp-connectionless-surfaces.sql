-- atlas:txmode none

-- Modify "platform_mcp_distributions" table
ALTER TABLE "platform_mcp_distributions" ADD CONSTRAINT "platform_mcp_distributions_connection_generation_check" CHECK ((connection_id IS NULL) = (connection_generation IS NULL)) NOT VALID, ALTER COLUMN "connection_id" DROP NOT NULL, ALTER COLUMN "connection_generation" DROP NOT NULL, ADD COLUMN "user_id" text NULL, ADD COLUMN "acting_surface" text NULL;
ALTER TABLE "platform_mcp_distributions" VALIDATE CONSTRAINT "platform_mcp_distributions_connection_generation_check";
-- Modify "platform_mcp_readiness" table
ALTER TABLE "platform_mcp_readiness" ADD CONSTRAINT "platform_mcp_readiness_connection_generation_check" CHECK ((connection_id IS NULL) = (connection_generation IS NULL)) NOT VALID, ALTER COLUMN "connection_id" DROP NOT NULL, ALTER COLUMN "connection_generation" DROP NOT NULL, ADD COLUMN "user_id" text NULL, ADD COLUMN "acting_surface" text NULL;
ALTER TABLE "platform_mcp_readiness" VALIDATE CONSTRAINT "platform_mcp_readiness_connection_generation_check";
-- Modify "platform_mcp_setup_handoffs" table
ALTER TABLE "platform_mcp_setup_handoffs" ADD CONSTRAINT "platform_mcp_setup_handoffs_connection_generation_check" CHECK ((connection_id IS NULL) = (connection_generation IS NULL)) NOT VALID, ALTER COLUMN "connection_id" DROP NOT NULL, ALTER COLUMN "connection_generation" DROP NOT NULL, ADD COLUMN "user_id" text NULL, ADD COLUMN "acting_surface" text NULL;
ALTER TABLE "platform_mcp_setup_handoffs" VALIDATE CONSTRAINT "platform_mcp_setup_handoffs_connection_generation_check";
