-- Modify "device_integration_configs" table
ALTER TABLE "device_integration_configs" ADD COLUMN "credentials_org_encrypted" text NULL;
-- Set comment to column: "credentials_org_encrypted" on table: "device_integration_configs"
COMMENT ON COLUMN "device_integration_configs"."credentials_org_encrypted" IS '@access: secret-restricted';
-- Modify "network_ingresses" table
ALTER TABLE "network_ingresses" ADD COLUMN "credentials_org_encrypted" text NULL;
-- Set comment to column: "credentials_org_encrypted" on table: "network_ingresses"
COMMENT ON COLUMN "network_ingresses"."credentials_org_encrypted" IS '@access: secret-restricted';
-- Modify "slack_directory_connections" table
ALTER TABLE "slack_directory_connections" ADD COLUMN "credentials_org_encrypted" text NULL;
-- Set comment to column: "credentials_org_encrypted" on table: "slack_directory_connections"
COMMENT ON COLUMN "slack_directory_connections"."credentials_org_encrypted" IS '@access: secret-restricted';
