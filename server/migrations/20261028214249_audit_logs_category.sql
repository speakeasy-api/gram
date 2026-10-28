-- atlas:txmode none

-- Modify "audit_logs" table
ALTER TABLE "audit_logs" ADD CONSTRAINT "audit_logs_category_check" CHECK (category = ANY (ARRAY['configuration'::text, 'security'::text])) NOT VALID, ADD COLUMN "category" text NOT NULL DEFAULT 'configuration';
ALTER TABLE "audit_logs" VALIDATE CONSTRAINT "audit_logs_category_check";
-- Set comment to column: "category" on table: "audit_logs"
COMMENT ON COLUMN "audit_logs"."category" IS '@access: confidential';
