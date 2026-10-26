-- Columns that application code no longer reads or writes but that are still in
-- schema.sql until their drop migration ships. sqlc packages that list this file
-- after schema.sql generate code as if these columns were already gone, so their
-- expanded `SELECT *` and `RETURNING *` never name them and keep working once the
-- columns are dropped. Remove each statement in the migration that drops its
-- column.

ALTER TABLE toolsets DROP COLUMN oauth_proxy_server_id;

-- The toolset MCP hosting columns (AIM-671). The canonical hosted MCP server owns hosting state; only the legacy
-- alias package (internal/hostedmcp/legacyalias) still reads mcp_slug and custom_domain_id, and it leaves this
-- file out of its schema.
ALTER TABLE toolsets DROP COLUMN mcp_slug;
ALTER TABLE toolsets DROP COLUMN mcp_is_public;
ALTER TABLE toolsets DROP COLUMN mcp_enabled;
ALTER TABLE toolsets DROP COLUMN custom_domain_id;
ALTER TABLE toolsets DROP COLUMN user_session_issuer_id;
