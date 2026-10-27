-- Modify "organization_metadata" table
ALTER TABLE "organization_metadata" ADD COLUMN "gateway_discovery_mode" text NULL;
-- Set comment to column: "gateway_discovery_mode" on table: "organization_metadata"
COMMENT ON COLUMN "organization_metadata"."gateway_discovery_mode" IS '@access: confidential';
