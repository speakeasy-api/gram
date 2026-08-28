-- atlas:txmode none

-- Create index "audit_logs_organization_subject_action_seq_idx" to table: "audit_logs"
CREATE INDEX CONCURRENTLY "audit_logs_organization_subject_action_seq_idx" ON "audit_logs" ("organization_id", "subject_type", "subject_id", "action", "seq" DESC);
-- Create "killswitch_customer_list_watermarks" table
CREATE TABLE "killswitch_customer_list_watermarks" (
  "organization_id" text NOT NULL,
  "definition_key" text NOT NULL,
  "principal_kind" text NOT NULL,
  "resource_kind" text NOT NULL,
  "watermark" bigint NOT NULL DEFAULT 0,
  PRIMARY KEY ("organization_id", "definition_key", "principal_kind", "resource_kind"),
  CONSTRAINT "killswitch_customer_list_watermarks_organization_id_fkey" FOREIGN KEY ("organization_id") REFERENCES "organization_metadata" ("id") ON UPDATE NO ACTION ON DELETE CASCADE,
  CONSTRAINT "killswitch_customer_list_watermarks_watermark_check" CHECK (watermark >= 0)
);
-- Create "killswitch_prescriptions" table
CREATE TABLE "killswitch_prescriptions" (
  "id" uuid NOT NULL DEFAULT generate_uuidv7(),
  "organization_id" text NOT NULL,
  "definition_key" text NOT NULL,
  "principal_kind" text NOT NULL,
  "principal_key" text NOT NULL,
  "resource_kind" text NOT NULL,
  "current_version" bigint NOT NULL,
  "created_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "updated_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  PRIMARY KEY ("id"),
  CONSTRAINT "killswitch_prescriptions_organization_id_fkey" FOREIGN KEY ("organization_id") REFERENCES "organization_metadata" ("id") ON UPDATE NO ACTION ON DELETE CASCADE,
  CONSTRAINT "killswitch_prescriptions_current_version_check" CHECK (current_version > 0),
  CONSTRAINT "killswitch_prescriptions_definition_key_check" CHECK (definition_key <> ''::text),
  CONSTRAINT "killswitch_prescriptions_principal_key_check" CHECK (principal_key <> ''::text),
  CONSTRAINT "killswitch_prescriptions_principal_kind_check" CHECK (principal_kind <> ''::text),
  CONSTRAINT "killswitch_prescriptions_resource_kind_check" CHECK (resource_kind <> ''::text)
);
-- Create index "killswitch_prescriptions_customer_list_idx" to table: "killswitch_prescriptions"
CREATE INDEX "killswitch_prescriptions_customer_list_idx" ON "killswitch_prescriptions" ("organization_id", "definition_key", "principal_kind", "resource_kind", "created_at" DESC, "id" DESC);
-- Create index "killswitch_prescriptions_evaluator_idx" to table: "killswitch_prescriptions"
CREATE INDEX "killswitch_prescriptions_evaluator_idx" ON "killswitch_prescriptions" ("organization_id", "definition_key", "principal_kind", "principal_key", "resource_kind", "id");
-- Create index "killswitch_prescriptions_organization_id_id_key" to table: "killswitch_prescriptions"
CREATE UNIQUE INDEX "killswitch_prescriptions_organization_id_id_key" ON "killswitch_prescriptions" ("organization_id", "id");
-- Create "killswitch_prescription_versions" table
CREATE TABLE "killswitch_prescription_versions" (
  "organization_id" text NOT NULL,
  "prescription_id" uuid NOT NULL,
  "version" bigint NOT NULL,
  "state" text NOT NULL,
  "resource_scope" text NOT NULL,
  "start_mode" text NULL,
  "starts_at" timestamptz NOT NULL,
  "expires_at" timestamptz NULL,
  "activated_at" timestamptz NULL,
  "superseded_at" timestamptz NULL,
  "internal_note" text NOT NULL,
  "external_note" text NOT NULL,
  "list_watermark" bigint NOT NULL DEFAULT 0,
  "created_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  PRIMARY KEY ("prescription_id", "version"),
  CONSTRAINT "killswitch_prescription_versions_prescription_fkey" FOREIGN KEY ("organization_id", "prescription_id") REFERENCES "killswitch_prescriptions" ("organization_id", "id") ON UPDATE NO ACTION ON DELETE CASCADE,
  CONSTRAINT "killswitch_prescription_versions_external_note_check" CHECK ((char_length(external_note) >= 1) AND (char_length(external_note) <= 500)),
  CONSTRAINT "killswitch_prescription_versions_internal_note_check" CHECK ((char_length(internal_note) >= 1) AND (char_length(internal_note) <= 4000)),
  CONSTRAINT "killswitch_prescription_versions_interval_check" CHECK ((expires_at IS NULL) OR (expires_at > starts_at)),
  CONSTRAINT "killswitch_prescription_versions_list_watermark_check" CHECK (list_watermark >= 0),
  CONSTRAINT "killswitch_prescription_versions_resource_scope_check" CHECK (resource_scope = ANY (ARRAY['all'::text, 'selected'::text])),
  CONSTRAINT "killswitch_prescription_versions_start_mode_check" CHECK (start_mode = ANY (ARRAY['now'::text, 'at'::text])),
  CONSTRAINT "killswitch_prescription_versions_state_check" CHECK (state <> ''::text),
  CONSTRAINT "killswitch_prescription_versions_version_check" CHECK (version > 0)
);
-- Create index "killswitch_prescription_versions_expiry_due_idx" to table: "killswitch_prescription_versions"
CREATE INDEX "killswitch_prescription_versions_expiry_due_idx" ON "killswitch_prescription_versions" ("expires_at", "prescription_id", "version") WHERE ((state = 'active'::text) AND (expires_at IS NOT NULL) AND ((superseded_at IS NULL) OR (expires_at < superseded_at)));
-- Create index "killswitch_prescription_versions_org_prescription_version_key" to table: "killswitch_prescription_versions"
CREATE UNIQUE INDEX "killswitch_prescription_versions_org_prescription_version_key" ON "killswitch_prescription_versions" ("organization_id", "prescription_id", "version");
-- Create "killswitch_expiry_events" table
CREATE TABLE "killswitch_expiry_events" (
  "organization_id" text NOT NULL,
  "prescription_id" uuid NOT NULL,
  "version" bigint NOT NULL,
  "recorded_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  PRIMARY KEY ("prescription_id", "version"),
  CONSTRAINT "killswitch_expiry_events_prescription_version_fkey" FOREIGN KEY ("organization_id", "prescription_id", "version") REFERENCES "killswitch_prescription_versions" ("organization_id", "prescription_id", "version") ON UPDATE NO ACTION ON DELETE CASCADE
);
-- Create "killswitch_operations" table
CREATE TABLE "killswitch_operations" (
  "organization_id" text NOT NULL,
  "operation_id" uuid NOT NULL,
  "actor_user_id" text NOT NULL,
  "operation" text NOT NULL,
  "request_hash" text NOT NULL,
  "status" text NOT NULL DEFAULT 'pending',
  "response" jsonb NULL,
  "expires_at" timestamptz NOT NULL,
  "created_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "updated_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  PRIMARY KEY ("organization_id", "operation_id"),
  CONSTRAINT "killswitch_operations_organization_id_fkey" FOREIGN KEY ("organization_id") REFERENCES "organization_metadata" ("id") ON UPDATE NO ACTION ON DELETE CASCADE,
  CONSTRAINT "killswitch_operations_actor_user_id_check" CHECK (actor_user_id <> ''::text),
  CONSTRAINT "killswitch_operations_completed_response_check" CHECK (((status = 'pending'::text) AND (response IS NULL)) OR ((status = 'completed'::text) AND (response IS NOT NULL))),
  CONSTRAINT "killswitch_operations_operation_check" CHECK (operation <> ''::text),
  CONSTRAINT "killswitch_operations_request_hash_check" CHECK (request_hash <> ''::text),
  CONSTRAINT "killswitch_operations_status_check" CHECK (status = ANY (ARRAY['pending'::text, 'completed'::text]))
);
-- Create index "killswitch_operations_expires_at_idx" to table: "killswitch_operations"
CREATE INDEX "killswitch_operations_expires_at_idx" ON "killswitch_operations" ("expires_at");
-- Create "killswitch_prescription_version_resources" table
CREATE TABLE "killswitch_prescription_version_resources" (
  "organization_id" text NOT NULL,
  "prescription_id" uuid NOT NULL,
  "version" bigint NOT NULL,
  "resource_key" text NOT NULL,
  PRIMARY KEY ("prescription_id", "version", "resource_key"),
  CONSTRAINT "killswitch_prescription_version_resources_version_fkey" FOREIGN KEY ("organization_id", "prescription_id", "version") REFERENCES "killswitch_prescription_versions" ("organization_id", "prescription_id", "version") ON UPDATE NO ACTION ON DELETE CASCADE,
  CONSTRAINT "killswitch_prescription_version_resources_resource_key_check" CHECK (resource_key <> ''::text)
);
-- Create index "killswitch_prescription_version_resources_lookup_idx" to table: "killswitch_prescription_version_resources"
CREATE INDEX "killswitch_prescription_version_resources_lookup_idx" ON "killswitch_prescription_version_resources" ("organization_id", "resource_key", "prescription_id", "version");
