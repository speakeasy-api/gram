-- atlas:nolint DS103
-- Modify "skill_versions" table
ALTER TABLE "skill_versions" DROP CONSTRAINT "skill_versions_asset_format_check", ADD CONSTRAINT "skill_versions_asset_format_check" CHECK (asset_format = 'zip'::text);
-- Modify "skills" table
ALTER TABLE "skills" DROP CONSTRAINT "skills_state_check", DROP COLUMN "state";
