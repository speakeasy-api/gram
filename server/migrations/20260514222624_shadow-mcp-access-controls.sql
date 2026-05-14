-- Create "shadow_mcp_approval_requests" table
CREATE TABLE "shadow_mcp_approval_requests" (
  "id" uuid NOT NULL DEFAULT generate_uuidv7(),
  "organization_id" text NOT NULL,
  "project_id" uuid NOT NULL,
  "requester_user_id" text NULL,
  "requester_email" text NULL,
  "requester_display_name" text NULL,
  "status" text NOT NULL,
  "risk_policy_id" uuid NULL,
  "risk_result_id" uuid NULL,
  "observed_name" text NULL,
  "observed_full_url" text NULL,
  "observed_url_host" text NULL,
  "observed_server_identity" text NULL,
  "request_fingerprint" text NULL,
  "tool_name" text NULL,
  "tool_call" text NULL,
  "block_reason" text NULL,
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
  CONSTRAINT "shadow_mcp_approval_requests_organization_id_fkey" FOREIGN KEY ("organization_id") REFERENCES "organization_metadata" ("id") ON UPDATE NO ACTION ON DELETE CASCADE,
  CONSTRAINT "shadow_mcp_approval_requests_project_id_fkey" FOREIGN KEY ("project_id") REFERENCES "projects" ("id") ON UPDATE NO ACTION ON DELETE CASCADE,
  CONSTRAINT "shadow_mcp_approval_requests_risk_policy_id_fkey" FOREIGN KEY ("risk_policy_id") REFERENCES "risk_policies" ("id") ON UPDATE NO ACTION ON DELETE SET NULL,
  CONSTRAINT "shadow_mcp_approval_requests_risk_result_id_fkey" FOREIGN KEY ("risk_result_id") REFERENCES "risk_results" ("id") ON UPDATE NO ACTION ON DELETE SET NULL,
  CONSTRAINT "shadow_mcp_approval_requests_blocked_count_check" CHECK (blocked_count > 0),
  CONSTRAINT "shadow_mcp_approval_requests_decision_check" CHECK (((status = 'requested'::text) AND (decided_at IS NULL)) OR ((status = ANY (ARRAY['approved'::text, 'denied'::text])) AND (decided_at IS NOT NULL))),
  CONSTRAINT "shadow_mcp_approval_requests_observed_full_url_check" CHECK ((observed_full_url IS NULL) OR (observed_full_url <> ''::text)),
  CONSTRAINT "shadow_mcp_approval_requests_observed_identity_check" CHECK ((observed_full_url IS NOT NULL) OR (observed_url_host IS NOT NULL) OR (observed_server_identity IS NOT NULL)),
  CONSTRAINT "shadow_mcp_approval_requests_observed_name_check" CHECK ((observed_name IS NULL) OR (observed_name <> ''::text)),
  CONSTRAINT "shadow_mcp_approval_requests_observed_server_identity_check" CHECK ((observed_server_identity IS NULL) OR (observed_server_identity <> ''::text)),
  CONSTRAINT "shadow_mcp_approval_requests_observed_url_host_check" CHECK ((observed_url_host IS NULL) OR (observed_url_host <> ''::text)),
  CONSTRAINT "shadow_mcp_approval_requests_request_fingerprint_check" CHECK ((request_fingerprint IS NULL) OR (request_fingerprint <> ''::text)),
  CONSTRAINT "shadow_mcp_approval_requests_status_check" CHECK (status = ANY (ARRAY['requested'::text, 'approved'::text, 'denied'::text])),
  CONSTRAINT "shadow_mcp_approval_requests_tool_name_check" CHECK ((tool_name IS NULL) OR (tool_name <> ''::text))
);
-- Create index "shadow_mcp_approval_requests_active_requester_fingerprint_key" to table: "shadow_mcp_approval_requests"
CREATE UNIQUE INDEX "shadow_mcp_approval_requests_active_requester_fingerprint_key" ON "shadow_mcp_approval_requests" ("organization_id", "project_id", "requester_user_id", "request_fingerprint") WHERE ((deleted IS FALSE) AND (status = 'requested'::text) AND (requester_user_id IS NOT NULL) AND (request_fingerprint IS NOT NULL));
-- Create index "shadow_mcp_approval_requests_organization_status_requested_idx" to table: "shadow_mcp_approval_requests"
CREATE INDEX "shadow_mcp_approval_requests_organization_status_requested_idx" ON "shadow_mcp_approval_requests" ("organization_id", "status", "requested_at" DESC) WHERE (deleted IS FALSE);
-- Create index "shadow_mcp_approval_requests_project_status_requested_idx" to table: "shadow_mcp_approval_requests"
CREATE INDEX "shadow_mcp_approval_requests_project_status_requested_idx" ON "shadow_mcp_approval_requests" ("project_id", "status", "requested_at" DESC) WHERE (deleted IS FALSE);
-- Create "shadow_mcp_access_rules" table
CREATE TABLE "shadow_mcp_access_rules" (
  "id" uuid NOT NULL DEFAULT generate_uuidv7(),
  "organization_id" text NOT NULL,
  "project_id" uuid NULL,
  "access_scope" text NOT NULL DEFAULT 'organization',
  "disposition" text NOT NULL,
  "match_breadth" text NOT NULL,
  "match_value" text NOT NULL,
  "display_name" text NOT NULL,
  "observed_full_url" text NULL,
  "observed_url_host" text NULL,
  "observed_server_identity" text NULL,
  "source_request_id" uuid NULL,
  "created_by" text NULL,
  "updated_by" text NULL,
  "reason" text NULL,
  "created_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "updated_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "deleted_at" timestamptz NULL,
  "deleted" boolean NOT NULL GENERATED ALWAYS AS (deleted_at IS NOT NULL) STORED,
  PRIMARY KEY ("id"),
  CONSTRAINT "shadow_mcp_access_rules_organization_id_fkey" FOREIGN KEY ("organization_id") REFERENCES "organization_metadata" ("id") ON UPDATE NO ACTION ON DELETE CASCADE,
  CONSTRAINT "shadow_mcp_access_rules_project_id_fkey" FOREIGN KEY ("project_id") REFERENCES "projects" ("id") ON UPDATE NO ACTION ON DELETE CASCADE,
  CONSTRAINT "shadow_mcp_access_rules_source_request_id_fkey" FOREIGN KEY ("source_request_id") REFERENCES "shadow_mcp_approval_requests" ("id") ON UPDATE NO ACTION ON DELETE SET NULL,
  CONSTRAINT "shadow_mcp_access_rules_access_scope_check" CHECK (access_scope = ANY (ARRAY['organization'::text, 'project'::text])),
  CONSTRAINT "shadow_mcp_access_rules_display_name_check" CHECK (display_name <> ''::text),
  CONSTRAINT "shadow_mcp_access_rules_disposition_check" CHECK (disposition = ANY (ARRAY['allowed'::text, 'denied'::text])),
  CONSTRAINT "shadow_mcp_access_rules_match_breadth_check" CHECK (match_breadth = ANY (ARRAY['full_url'::text, 'url_host'::text, 'server_identity'::text])),
  CONSTRAINT "shadow_mcp_access_rules_match_value_check" CHECK (match_value <> ''::text),
  CONSTRAINT "shadow_mcp_access_rules_observed_full_url_check" CHECK ((observed_full_url IS NULL) OR (observed_full_url <> ''::text)),
  CONSTRAINT "shadow_mcp_access_rules_observed_server_identity_check" CHECK ((observed_server_identity IS NULL) OR (observed_server_identity <> ''::text)),
  CONSTRAINT "shadow_mcp_access_rules_observed_url_host_check" CHECK ((observed_url_host IS NULL) OR (observed_url_host <> ''::text)),
  CONSTRAINT "shadow_mcp_access_rules_scope_project_check" CHECK (((access_scope = 'organization'::text) AND (project_id IS NULL)) OR ((access_scope = 'project'::text) AND (project_id IS NOT NULL)))
);
-- Create index "shadow_mcp_access_rules_organization_disposition_created_idx" to table: "shadow_mcp_access_rules"
CREATE INDEX "shadow_mcp_access_rules_organization_disposition_created_idx" ON "shadow_mcp_access_rules" ("organization_id", "disposition", "created_at" DESC) WHERE (deleted IS FALSE);
-- Create index "shadow_mcp_access_rules_organization_scope_match_value_key" to table: "shadow_mcp_access_rules"
CREATE UNIQUE INDEX "shadow_mcp_access_rules_organization_scope_match_value_key" ON "shadow_mcp_access_rules" ("organization_id", "match_breadth", "match_value") WHERE ((deleted IS FALSE) AND (access_scope = 'organization'::text));
-- Create index "shadow_mcp_access_rules_project_scope_created_idx" to table: "shadow_mcp_access_rules"
CREATE INDEX "shadow_mcp_access_rules_project_scope_created_idx" ON "shadow_mcp_access_rules" ("organization_id", "project_id", "disposition", "created_at" DESC) WHERE (deleted IS FALSE);
-- Create index "shadow_mcp_access_rules_project_scope_match_value_key" to table: "shadow_mcp_access_rules"
CREATE UNIQUE INDEX "shadow_mcp_access_rules_project_scope_match_value_key" ON "shadow_mcp_access_rules" ("organization_id", "project_id", "match_breadth", "match_value") WHERE ((deleted IS FALSE) AND (access_scope = 'project'::text));
-- Create index "shadow_mcp_access_rules_source_request_id_idx" to table: "shadow_mcp_access_rules"
CREATE INDEX "shadow_mcp_access_rules_source_request_id_idx" ON "shadow_mcp_access_rules" ("source_request_id") WHERE ((source_request_id IS NOT NULL) AND (deleted IS FALSE));
