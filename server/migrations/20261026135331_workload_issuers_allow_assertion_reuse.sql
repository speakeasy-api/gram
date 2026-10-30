-- Modify "workload_issuers" table
ALTER TABLE "workload_issuers" ADD COLUMN "allow_assertion_reuse" boolean NULL DEFAULT false;
-- Set comment to column: "allow_assertion_reuse" on table: "workload_issuers"
COMMENT ON COLUMN "workload_issuers"."allow_assertion_reuse" IS 'Whether assertions carrying a jti or uti may be exchanged repeatedly until expiry. NULL is false.
@access: confidential';
