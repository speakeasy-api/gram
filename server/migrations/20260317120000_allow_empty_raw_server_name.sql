-- Allow empty raw_server_name in hooks_server_name_overrides
-- This enables overriding the display name for local tool calls (which have an empty server name)
ALTER TABLE hooks_server_name_overrides DROP CONSTRAINT IF EXISTS hooks_server_name_overrides_raw_server_name_check;
