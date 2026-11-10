-- Modify "organization_metadata" table
ALTER TABLE "organization_metadata" ADD CONSTRAINT "organization_metadata_adoption_dashboard_url_check" CHECK (char_length(adoption_dashboard_url) <= 2048), ADD COLUMN "adoption_dashboard_url" text NULL;
-- Set comment to column: "adoption_dashboard_url" on table: "organization_metadata"
COMMENT ON COLUMN "organization_metadata"."adoption_dashboard_url" IS 'Staff-entered link to the adoption dashboard for this organization.
@access: restricted';
