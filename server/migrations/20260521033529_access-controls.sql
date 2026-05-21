-- Create "access_approval_requests" table
CREATE TABLE "access_approval_requests" (
  "id" uuid NOT NULL DEFAULT generate_uuidv7(),
  "organization_id" text NOT NULL,
  "project_id" uuid NOT NULL,
  "resource_type" text NOT NULL,
  "requester_user_id" text NULL,
  "requester_email" text NULL,
  "requester_display_name" text NULL,
  "status" text NOT NULL,
  "request_fingerprint" text NULL,
  "display_name" text NULL,
  "observed_summary" jsonb NOT NULL DEFAULT '{}',
  "blocked_count" integer NOT NULL DEFAULT 1,
  "first_blocked_at" timestamptz NULL,
  "last_blocked_at" timestamptz NULL,
  "requested_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "decided_at" timestamptz NULL,
  "decided_by" text NULL,
  "decision_note" text NULL,
  "created_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "updated_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "deleted_at" timestamptz NULL,
  "deleted" boolean NOT NULL GENERATED ALWAYS AS (deleted_at IS NOT NULL) STORED,
  PRIMARY KEY ("id"),
  CONSTRAINT "access_approval_requests_organization_id_fkey" FOREIGN KEY ("organization_id") REFERENCES "organization_metadata" ("id") ON UPDATE NO ACTION ON DELETE CASCADE,
  CONSTRAINT "access_approval_requests_project_id_fkey" FOREIGN KEY ("project_id") REFERENCES "projects" ("id") ON UPDATE NO ACTION ON DELETE CASCADE,
  CONSTRAINT "access_approval_requests_blocked_count_check" CHECK (blocked_count > 0),
  CONSTRAINT "access_approval_requests_decision_check" CHECK (((status = 'requested'::text) AND (decided_at IS NULL)) OR ((status = ANY (ARRAY['approved'::text, 'denied'::text])) AND (decided_at IS NOT NULL))),
  CONSTRAINT "access_approval_requests_display_name_check" CHECK ((display_name IS NULL) OR (display_name <> ''::text)),
  CONSTRAINT "access_approval_requests_observed_summary_check" CHECK (jsonb_typeof(observed_summary) = 'object'::text),
  CONSTRAINT "access_approval_requests_request_fingerprint_check" CHECK ((request_fingerprint IS NULL) OR (request_fingerprint <> ''::text)),
  CONSTRAINT "access_approval_requests_resource_type_check" CHECK (resource_type <> ''::text),
  CONSTRAINT "access_approval_requests_status_check" CHECK (status = ANY (ARRAY['requested'::text, 'approved'::text, 'denied'::text]))
);
-- Create index "access_approval_requests_active_requester_fingerprint_key" to table: "access_approval_requests"
CREATE UNIQUE INDEX "access_approval_requests_active_requester_fingerprint_key" ON "access_approval_requests" ("organization_id", "project_id", "resource_type", "requester_user_id", "request_fingerprint") WHERE ((deleted IS FALSE) AND (status = 'requested'::text) AND (requester_user_id IS NOT NULL) AND (request_fingerprint IS NOT NULL));
-- Create index "access_approval_requests_organization_resource_status_requested" to table: "access_approval_requests"
CREATE INDEX "access_approval_requests_organization_resource_status_requested" ON "access_approval_requests" ("organization_id", "resource_type", "status", "requested_at" DESC) WHERE (deleted IS FALSE);
-- Create index "access_approval_requests_project_resource_status_requested_idx" to table: "access_approval_requests"
CREATE INDEX "access_approval_requests_project_resource_status_requested_idx" ON "access_approval_requests" ("project_id", "resource_type", "status", "requested_at" DESC) WHERE (deleted IS FALSE);
-- Create "access_rules" table
CREATE TABLE "access_rules" (
  "id" uuid NOT NULL DEFAULT generate_uuidv7(),
  "organization_id" text NOT NULL,
  "project_id" uuid NULL,
  "access_scope" text NOT NULL DEFAULT 'organization',
  "resource_type" text NOT NULL,
  "disposition" text NOT NULL,
  "match_kind" text NOT NULL,
  "match_value" text NOT NULL,
  "display_name" text NOT NULL,
  "observed_summary" jsonb NOT NULL DEFAULT '{}',
  "source_request_id" uuid NULL,
  "created_by" text NULL,
  "updated_by" text NULL,
  "reason" text NULL,
  "created_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "updated_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "deleted_at" timestamptz NULL,
  "deleted" boolean NOT NULL GENERATED ALWAYS AS (deleted_at IS NOT NULL) STORED,
  PRIMARY KEY ("id"),
  CONSTRAINT "access_rules_organization_id_fkey" FOREIGN KEY ("organization_id") REFERENCES "organization_metadata" ("id") ON UPDATE NO ACTION ON DELETE CASCADE,
  CONSTRAINT "access_rules_project_id_fkey" FOREIGN KEY ("project_id") REFERENCES "projects" ("id") ON UPDATE NO ACTION ON DELETE CASCADE,
  CONSTRAINT "access_rules_source_request_id_fkey" FOREIGN KEY ("source_request_id") REFERENCES "access_approval_requests" ("id") ON UPDATE NO ACTION ON DELETE SET NULL,
  CONSTRAINT "access_rules_access_scope_check" CHECK (access_scope = ANY (ARRAY['organization'::text, 'project'::text])),
  CONSTRAINT "access_rules_display_name_check" CHECK (display_name <> ''::text),
  CONSTRAINT "access_rules_disposition_check" CHECK (disposition = ANY (ARRAY['allowed'::text, 'denied'::text])),
  CONSTRAINT "access_rules_match_kind_check" CHECK (match_kind <> ''::text),
  CONSTRAINT "access_rules_match_value_check" CHECK (match_value <> ''::text),
  CONSTRAINT "access_rules_observed_summary_check" CHECK (jsonb_typeof(observed_summary) = 'object'::text),
  CONSTRAINT "access_rules_resource_type_check" CHECK (resource_type <> ''::text),
  CONSTRAINT "access_rules_scope_project_check" CHECK (((access_scope = 'organization'::text) AND (project_id IS NULL)) OR ((access_scope = 'project'::text) AND (project_id IS NOT NULL)))
);
-- Create index "access_rules_organization_disposition_created_idx" to table: "access_rules"
CREATE INDEX "access_rules_organization_disposition_created_idx" ON "access_rules" ("organization_id", "resource_type", "disposition", "created_at" DESC) WHERE (deleted IS FALSE);
-- Create index "access_rules_organization_scope_match_value_key" to table: "access_rules"
CREATE UNIQUE INDEX "access_rules_organization_scope_match_value_key" ON "access_rules" ("organization_id", "resource_type", "match_kind", "match_value") WHERE ((deleted IS FALSE) AND (access_scope = 'organization'::text));
-- Create index "access_rules_project_scope_created_idx" to table: "access_rules"
CREATE INDEX "access_rules_project_scope_created_idx" ON "access_rules" ("organization_id", "project_id", "resource_type", "disposition", "created_at" DESC) WHERE (deleted IS FALSE);
-- Create index "access_rules_project_scope_match_value_key" to table: "access_rules"
CREATE UNIQUE INDEX "access_rules_project_scope_match_value_key" ON "access_rules" ("organization_id", "project_id", "resource_type", "match_kind", "match_value") WHERE ((deleted IS FALSE) AND (access_scope = 'project'::text));
-- Create index "access_rules_source_request_id_idx" to table: "access_rules"
CREATE INDEX "access_rules_source_request_id_idx" ON "access_rules" ("source_request_id") WHERE ((source_request_id IS NOT NULL) AND (deleted IS FALSE));
