-- Set comment to column: "private_key_encrypted" on table: "mcp_auth_client_github_apps"
COMMENT ON COLUMN "mcp_auth_client_github_apps"."private_key_encrypted" IS '@access: secret-restricted';
