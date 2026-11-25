-- atlas:txmode none

-- Create index "mcp_auth_clients_type_organization_key" to table: "mcp_auth_clients"
CREATE UNIQUE INDEX CONCURRENTLY "mcp_auth_clients_type_organization_key" ON "mcp_auth_clients" ("id", "client_type", "organization_id");
-- Create "mcp_auth_client_github_apps" table
CREATE TABLE "mcp_auth_client_github_apps" (
  "auth_client_id" uuid NOT NULL,
  "organization_id" text NOT NULL,
  "client_type" text NOT NULL DEFAULT 'service_account',
  "instance_url" text NOT NULL,
  "app_id" bigint NOT NULL,
  "installation_id" bigint NOT NULL,
  "private_key_encrypted" text NOT NULL,
  "upstream_origin" text NOT NULL,
  "credential_version" uuid NOT NULL DEFAULT generate_uuidv7(),
  "created_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "updated_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  PRIMARY KEY ("auth_client_id"),
  CONSTRAINT "mcp_auth_client_github_apps_client_fkey" FOREIGN KEY ("auth_client_id", "client_type", "organization_id") REFERENCES "mcp_auth_clients" ("id", "client_type", "organization_id") ON UPDATE NO ACTION ON DELETE CASCADE,
  CONSTRAINT "mcp_auth_client_github_apps_app_id_check" CHECK (app_id > 0),
  CONSTRAINT "mcp_auth_client_github_apps_client_type_check" CHECK (client_type = 'service_account'::text),
  CONSTRAINT "mcp_auth_client_github_apps_installation_id_check" CHECK (installation_id > 0),
  CONSTRAINT "mcp_auth_client_github_apps_instance_url_check" CHECK (instance_url <> ''::text),
  CONSTRAINT "mcp_auth_client_github_apps_private_key_encrypted_check" CHECK (private_key_encrypted <> ''::text),
  CONSTRAINT "mcp_auth_client_github_apps_upstream_origin_check" CHECK (upstream_origin <> ''::text)
);
-- Set comment to column: "auth_client_id" on table: "mcp_auth_client_github_apps"
COMMENT ON COLUMN "mcp_auth_client_github_apps"."auth_client_id" IS '@access: confidential';
-- Set comment to column: "organization_id" on table: "mcp_auth_client_github_apps"
COMMENT ON COLUMN "mcp_auth_client_github_apps"."organization_id" IS '@access: confidential';
-- Set comment to column: "client_type" on table: "mcp_auth_client_github_apps"
COMMENT ON COLUMN "mcp_auth_client_github_apps"."client_type" IS '@access: confidential';
-- Set comment to column: "instance_url" on table: "mcp_auth_client_github_apps"
COMMENT ON COLUMN "mcp_auth_client_github_apps"."instance_url" IS '@access: restricted';
-- Set comment to column: "app_id" on table: "mcp_auth_client_github_apps"
COMMENT ON COLUMN "mcp_auth_client_github_apps"."app_id" IS '@access: confidential';
-- Set comment to column: "installation_id" on table: "mcp_auth_client_github_apps"
COMMENT ON COLUMN "mcp_auth_client_github_apps"."installation_id" IS '@access: confidential';
-- Set comment to column: "private_key_encrypted" on table: "mcp_auth_client_github_apps"
COMMENT ON COLUMN "mcp_auth_client_github_apps"."private_key_encrypted" IS '@access: secret-restricted';
-- Set comment to column: "upstream_origin" on table: "mcp_auth_client_github_apps"
COMMENT ON COLUMN "mcp_auth_client_github_apps"."upstream_origin" IS '@access: restricted';
-- Set comment to column: "credential_version" on table: "mcp_auth_client_github_apps"
COMMENT ON COLUMN "mcp_auth_client_github_apps"."credential_version" IS '@access: confidential';
-- Set comment to column: "created_at" on table: "mcp_auth_client_github_apps"
COMMENT ON COLUMN "mcp_auth_client_github_apps"."created_at" IS '@access: confidential';
-- Set comment to column: "updated_at" on table: "mcp_auth_client_github_apps"
COMMENT ON COLUMN "mcp_auth_client_github_apps"."updated_at" IS '@access: confidential';
