-- Drop auto-increment from toolset_versions.version column
-- Atlas has difficulty with IDENTITY column changes, so this is done manually
ALTER TABLE toolset_versions ALTER COLUMN version DROP IDENTITY;
