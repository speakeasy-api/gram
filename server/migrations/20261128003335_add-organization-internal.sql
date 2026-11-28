-- Modify "organization_metadata" table
ALTER TABLE "organization_metadata" ADD COLUMN "internal" boolean NOT NULL DEFAULT false;
-- Set comment to column: "internal" on table: "organization_metadata"
COMMENT ON COLUMN "organization_metadata"."internal" IS 'Staff-set label for organizations owned by Speakeasy employees for testing and dogfooding.
@access: confidential';
