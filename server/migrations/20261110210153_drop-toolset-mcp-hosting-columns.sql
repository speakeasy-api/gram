-- Modify "toolsets" table
-- atlas:nolint DS103
ALTER TABLE "toolsets" DROP CONSTRAINT "toolsets_mcp_slug_check", DROP COLUMN "mcp_slug", DROP COLUMN "mcp_is_public", DROP COLUMN "custom_domain_id", DROP COLUMN "mcp_enabled", DROP COLUMN "user_session_issuer_id";
