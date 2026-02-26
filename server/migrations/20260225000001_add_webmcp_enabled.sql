-- Add webmcp_enabled column to mcp_metadata table.
-- When enabled, the install page injects a WebMCP script that registers
-- tools with navigator.modelContext for browsing agent discovery.
ALTER TABLE mcp_metadata ADD COLUMN webmcp_enabled BOOLEAN NOT NULL DEFAULT false;
