-- Drop auto-increment from toolset_versions.version column
ALTER TABLE toolset_versions ALTER COLUMN version DROP IDENTITY;