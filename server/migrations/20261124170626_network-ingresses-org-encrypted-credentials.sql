-- Modify "network_ingresses" table
ALTER TABLE "network_ingresses" ADD COLUMN "credentials_org_encrypted" text NULL;
-- Set comment to column: "credentials_org_encrypted" on table: "network_ingresses"
COMMENT ON COLUMN "network_ingresses"."credentials_org_encrypted" IS '@access: secret-restricted';
