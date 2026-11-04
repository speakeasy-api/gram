-- atlas:txmode none

-- Modify "principal_grants" table
ALTER TABLE "principal_grants" ADD COLUMN "expires_at" timestamptz NULL;
-- Create index "principal_grants_org_expires_at_idx" to table: "principal_grants"
CREATE INDEX CONCURRENTLY "principal_grants_org_expires_at_idx" ON "principal_grants" ("organization_id", "expires_at") WHERE (expires_at IS NOT NULL);
-- Set comment to column: "expires_at" on table: "principal_grants"
COMMENT ON COLUMN "principal_grants"."expires_at" IS 'When a temporary grant stops authorizing new requests. NULL = permanent. A row whose expires_at has passed grants nothing and is ignored by every reader.
@access: confidential';
