-- Add selected_remotes column to track user-selected remote URLs for multi-remote MCP servers
ALTER TABLE external_mcp_attachments
ADD COLUMN selected_remotes TEXT[];
