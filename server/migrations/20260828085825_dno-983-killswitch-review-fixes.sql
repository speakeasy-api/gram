-- atlas:txmode none

-- Create index "audit_logs_organization_subject_action_seq_idx" to table: "audit_logs"
CREATE INDEX CONCURRENTLY "audit_logs_organization_subject_action_seq_idx" ON "audit_logs" ("organization_id", "subject_type", "subject_id", "action", "seq" DESC);
-- Modify "killswitch_prescription_versions" table
ALTER TABLE "killswitch_prescription_versions" ADD CONSTRAINT "killswitch_prescription_versions_list_watermark_check" CHECK (list_watermark >= 0) NOT VALID, ADD CONSTRAINT "killswitch_prescription_versions_start_mode_check" CHECK (start_mode = ANY (ARRAY['now'::text, 'at'::text])) NOT VALID, ADD COLUMN "start_mode" text NULL, ADD COLUMN "list_watermark" bigint NOT NULL DEFAULT 0;
ALTER TABLE "killswitch_prescription_versions" VALIDATE CONSTRAINT "killswitch_prescription_versions_list_watermark_check";
ALTER TABLE "killswitch_prescription_versions" VALIDATE CONSTRAINT "killswitch_prescription_versions_start_mode_check";
-- Create index "killswitch_prescriptions_customer_list_idx" to table: "killswitch_prescriptions"
CREATE INDEX CONCURRENTLY "killswitch_prescriptions_customer_list_idx" ON "killswitch_prescriptions" ("organization_id", "definition_key", "principal_kind", "resource_kind", "created_at" DESC, "id" DESC);
-- Create "killswitch_customer_list_watermarks" table
CREATE TABLE "killswitch_customer_list_watermarks" (
  "organization_id" text NOT NULL,
  "definition_key" text NOT NULL,
  "principal_kind" text NOT NULL,
  "resource_kind" text NOT NULL,
  "watermark" bigint NOT NULL DEFAULT 0,
  PRIMARY KEY ("organization_id", "definition_key", "principal_kind", "resource_kind"),
  CONSTRAINT "killswitch_customer_list_watermarks_watermark_check" CHECK (watermark >= 0)
);
