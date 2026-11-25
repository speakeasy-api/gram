-- Modify "audit_logs" table
ALTER TABLE "audit_logs" ADD COLUMN "is_internal" boolean NOT NULL DEFAULT false;
-- Set comment to column: "is_internal" on table: "audit_logs"
COMMENT ON COLUMN "audit_logs"."is_internal" IS '@access: confidential';
-- Modify "security_audit_logs" table
ALTER TABLE "security_audit_logs" ADD COLUMN "is_internal" boolean NOT NULL DEFAULT false;
-- Set comment to column: "is_internal" on table: "security_audit_logs"
COMMENT ON COLUMN "security_audit_logs"."is_internal" IS '@access: confidential';
