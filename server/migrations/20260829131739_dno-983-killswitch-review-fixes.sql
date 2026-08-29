-- atlas:txmode none

-- Create index "audit_logs_organization_subject_action_seq_idx" to table: "audit_logs"
CREATE INDEX CONCURRENTLY "audit_logs_organization_subject_action_seq_idx" ON "audit_logs" ("organization_id", "subject_type", "subject_id", "action", "seq" DESC);
-- Create index "audit_logs_organization_subject_seq_idx" to table: "audit_logs"
CREATE INDEX CONCURRENTLY "audit_logs_organization_subject_seq_idx" ON "audit_logs" ("organization_id", "subject_id", "seq" DESC);
-- Modify "killswitch_prescription_versions" table
ALTER TABLE "killswitch_prescription_versions" DROP CONSTRAINT "killswitch_prescription_versions_interval_check", ADD CONSTRAINT "killswitch_prescription_versions_interval_check" CHECK ((expires_at IS NULL) OR (starts_at IS NULL) OR (expires_at > starts_at)) NOT VALID, ALTER COLUMN "starts_at" DROP NOT NULL;
ALTER TABLE "killswitch_prescription_versions" VALIDATE CONSTRAINT "killswitch_prescription_versions_interval_check";
-- Create index "killswitch_prescriptions_customer_list_idx" to table: "killswitch_prescriptions"
CREATE INDEX CONCURRENTLY "killswitch_prescriptions_customer_list_idx" ON "killswitch_prescriptions" ("organization_id", "definition_key", "principal_kind", "resource_kind", "created_at" DESC, "id" DESC);
