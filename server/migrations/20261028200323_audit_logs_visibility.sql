-- atlas:txmode none

-- Modify "audit_logs" table
ALTER TABLE "audit_logs" ADD CONSTRAINT "audit_logs_visibility_check" CHECK (visibility = ANY (ARRAY['customer'::text, 'internal'::text])) NOT VALID, ADD COLUMN "visibility" text NOT NULL DEFAULT 'customer';
ALTER TABLE "audit_logs" VALIDATE CONSTRAINT "audit_logs_visibility_check";
-- Set comment to column: "visibility" on table: "audit_logs"
COMMENT ON COLUMN "audit_logs"."visibility" IS '@access: confidential';
