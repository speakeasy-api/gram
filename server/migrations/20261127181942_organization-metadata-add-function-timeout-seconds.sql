-- Modify "organization_metadata" table
ALTER TABLE "organization_metadata" ADD COLUMN "function_timeout_seconds" integer NULL;
-- Set comment to column: "function_timeout_seconds" on table: "organization_metadata"
COMMENT ON COLUMN "organization_metadata"."function_timeout_seconds" IS 'Staff-set limit in seconds on one function invocation; NULL means the platform default.
@access: confidential';
