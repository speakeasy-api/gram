-- atlas:nolint DS103
-- Modify "toolsets" table
ALTER TABLE "toolsets" DROP CONSTRAINT "toolsets_oauth_exclusivity", DROP COLUMN "oauth_proxy_server_id";
