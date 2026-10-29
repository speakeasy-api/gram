-- Create "mcp_auth_client_github_apps" table
CREATE TABLE "mcp_auth_client_github_apps" (
  "auth_client_id" uuid NOT NULL,
  "organization_id" text NOT NULL,
  "instance_url" text NOT NULL,
  "app_id" bigint NOT NULL,
  "installation_id" bigint NOT NULL,
  "private_key_encrypted" text NOT NULL,
  "upstream_origin" text NOT NULL,
  "credential_version" uuid NOT NULL DEFAULT generate_uuidv7(),
  PRIMARY KEY ("auth_client_id"),
  CONSTRAINT "mcp_auth_client_github_apps_client_fkey" FOREIGN KEY ("auth_client_id", "organization_id") REFERENCES "mcp_auth_clients" ("id", "organization_id") ON UPDATE NO ACTION ON DELETE CASCADE,
  CONSTRAINT "mcp_auth_client_github_apps_app_id_check" CHECK (app_id > 0),
  CONSTRAINT "mcp_auth_client_github_apps_installation_id_check" CHECK (installation_id > 0),
  CONSTRAINT "mcp_auth_client_github_apps_instance_url_check" CHECK (instance_url <> ''::text),
  CONSTRAINT "mcp_auth_client_github_apps_private_key_encrypted_check" CHECK (private_key_encrypted <> ''::text),
  CONSTRAINT "mcp_auth_client_github_apps_upstream_origin_check" CHECK (upstream_origin <> ''::text)
);
-- Set comment to column: "auth_client_id" on table: "mcp_auth_client_github_apps"
COMMENT ON COLUMN "mcp_auth_client_github_apps"."auth_client_id" IS '@access: confidential';
-- Set comment to column: "organization_id" on table: "mcp_auth_client_github_apps"
COMMENT ON COLUMN "mcp_auth_client_github_apps"."organization_id" IS '@access: confidential';
-- Set comment to column: "instance_url" on table: "mcp_auth_client_github_apps"
COMMENT ON COLUMN "mcp_auth_client_github_apps"."instance_url" IS '@access: confidential';
-- Set comment to column: "app_id" on table: "mcp_auth_client_github_apps"
COMMENT ON COLUMN "mcp_auth_client_github_apps"."app_id" IS '@access: confidential';
-- Set comment to column: "installation_id" on table: "mcp_auth_client_github_apps"
COMMENT ON COLUMN "mcp_auth_client_github_apps"."installation_id" IS '@access: confidential';
-- Set comment to column: "private_key_encrypted" on table: "mcp_auth_client_github_apps"
COMMENT ON COLUMN "mcp_auth_client_github_apps"."private_key_encrypted" IS '@access: secret-restricted';
-- Set comment to column: "upstream_origin" on table: "mcp_auth_client_github_apps"
COMMENT ON COLUMN "mcp_auth_client_github_apps"."upstream_origin" IS '@access: confidential';
-- Set comment to column: "credential_version" on table: "mcp_auth_client_github_apps"
COMMENT ON COLUMN "mcp_auth_client_github_apps"."credential_version" IS '@access: confidential';
