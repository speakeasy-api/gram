-- Modify "toolsets" table
-- Hand-adjusted from Atlas's generated DROP COLUMN/ADD COLUMN pair into
-- RENAME COLUMN statements so the pre-swap publishing values remain
-- recoverable until the final drop migration. The end state is identical to
-- the generated form, so later `mise run db:diff` runs see no drift.
ALTER TABLE "toolsets" DROP CONSTRAINT "toolsets_mcp_slug_check";
DROP INDEX "toolsets_mcp_slug_custom_domain_id_key";
DROP INDEX "toolsets_mcp_slug_null_custom_domain_id_key";
ALTER TABLE "toolsets" RENAME COLUMN "mcp_slug" TO "deprecated_mcp_slug";
ALTER TABLE "toolsets" RENAME COLUMN "mcp_is_public" TO "deprecated_mcp_is_public";
ALTER TABLE "toolsets" RENAME COLUMN "mcp_enabled" TO "deprecated_mcp_enabled";
ALTER TABLE "toolsets" RENAME COLUMN "custom_domain_id" TO "deprecated_custom_domain_id";
