-- Create "mcp_approval_requests" table
CREATE TABLE "mcp_approval_requests" (
  "id" uuid NOT NULL DEFAULT generate_uuidv7(),
  "organization_id" text NOT NULL,
  "project_id" uuid NULL,
  "target_kind" text NOT NULL,
  "target_raw" text NOT NULL,
  "target_key" text NOT NULL,
  "artifact_ref" text NULL,
  "version_pinned" boolean NOT NULL DEFAULT false,
  "risk_policy_bypass_request_id" uuid NULL,
  "status" text NOT NULL DEFAULT 'requested',
  "current_evidence" jsonb NOT NULL DEFAULT '{}',
  "evidence_version" integer NOT NULL DEFAULT 1,
  "evidence_collected_at" timestamptz NULL,
  "created_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "updated_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "deleted_at" timestamptz NULL,
  "deleted" boolean NOT NULL GENERATED ALWAYS AS (deleted_at IS NOT NULL) STORED,
  PRIMARY KEY ("id"),
  CONSTRAINT "mcp_approval_requests_bypass_request_id_fkey" FOREIGN KEY ("risk_policy_bypass_request_id") REFERENCES "risk_policy_bypass_requests" ("id") ON UPDATE NO ACTION ON DELETE SET NULL,
  CONSTRAINT "mcp_approval_requests_organization_id_fkey" FOREIGN KEY ("organization_id") REFERENCES "organization_metadata" ("id") ON UPDATE NO ACTION ON DELETE CASCADE,
  CONSTRAINT "mcp_approval_requests_project_id_fkey" FOREIGN KEY ("project_id") REFERENCES "projects" ("id") ON UPDATE NO ACTION ON DELETE CASCADE,
  CONSTRAINT "mcp_approval_requests_current_evidence_check" CHECK (jsonb_typeof(current_evidence) = 'object'::text)
);
-- Create index "mcp_approval_requests_artifact_ref_idx" to table: "mcp_approval_requests"
CREATE INDEX "mcp_approval_requests_artifact_ref_idx" ON "mcp_approval_requests" ("artifact_ref") WHERE ((deleted IS FALSE) AND (artifact_ref IS NOT NULL));
-- Create index "mcp_approval_requests_bypass_request_id_idx" to table: "mcp_approval_requests"
CREATE INDEX "mcp_approval_requests_bypass_request_id_idx" ON "mcp_approval_requests" ("risk_policy_bypass_request_id");
-- Create index "mcp_approval_requests_id_organization_id_key" to table: "mcp_approval_requests"
CREATE UNIQUE INDEX "mcp_approval_requests_id_organization_id_key" ON "mcp_approval_requests" ("id", "organization_id");
-- Create index "mcp_approval_requests_org_status_updated_idx" to table: "mcp_approval_requests"
CREATE INDEX "mcp_approval_requests_org_status_updated_idx" ON "mcp_approval_requests" ("organization_id", "status", "updated_at" DESC) WHERE (deleted IS FALSE);
-- Create index "mcp_approval_requests_organization_id_idx" to table: "mcp_approval_requests"
CREATE INDEX "mcp_approval_requests_organization_id_idx" ON "mcp_approval_requests" ("organization_id");
-- Create index "mcp_approval_requests_organization_id_target_key" to table: "mcp_approval_requests"
CREATE UNIQUE INDEX "mcp_approval_requests_organization_id_target_key" ON "mcp_approval_requests" ("organization_id", "target_kind", "target_key") WHERE (deleted IS FALSE);
-- Create index "mcp_approval_requests_project_id_idx" to table: "mcp_approval_requests"
CREATE INDEX "mcp_approval_requests_project_id_idx" ON "mcp_approval_requests" ("project_id");
-- Set comment to table: "mcp_approval_requests"
COMMENT ON TABLE "mcp_approval_requests" IS 'One review per MCP server per organization. Re-requests reopen the same row so decisions accumulate as history, giving "have we decided on this before?" for free.';
-- Set comment to column: "artifact_ref" on table: "mcp_approval_requests"
COMMENT ON COLUMN "mcp_approval_requests"."artifact_ref" IS 'Resolved immutable artifact identity. NULL means unidentified, which must surface as unknown rather than as an absence of findings.';
-- Set comment to column: "version_pinned" on table: "mcp_approval_requests"
COMMENT ON COLUMN "mcp_approval_requests"."version_pinned" IS 'False for a floating invocation such as an unpinned npx command, where anything scanned may not be what runs.';
-- Create "mcp_research_reports" table
CREATE TABLE "mcp_research_reports" (
  "id" uuid NOT NULL DEFAULT generate_uuidv7(),
  "organization_id" text NOT NULL,
  "project_id" uuid NULL,
  "mcp_approval_request_id" uuid NOT NULL,
  "status" text NOT NULL DEFAULT 'running',
  "report" jsonb NOT NULL DEFAULT '{}',
  "report_version" integer NOT NULL DEFAULT 1,
  "model" text NULL,
  "prompt_version" text NULL,
  "requested_by" text NULL,
  "started_at" timestamptz NULL,
  "completed_at" timestamptz NULL,
  "error" text NULL,
  "created_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "updated_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "deleted_at" timestamptz NULL,
  "deleted" boolean NOT NULL GENERATED ALWAYS AS (deleted_at IS NOT NULL) STORED,
  PRIMARY KEY ("id"),
  CONSTRAINT "mcp_research_reports_organization_id_fkey" FOREIGN KEY ("organization_id") REFERENCES "organization_metadata" ("id") ON UPDATE NO ACTION ON DELETE CASCADE,
  CONSTRAINT "mcp_research_reports_project_id_fkey" FOREIGN KEY ("project_id") REFERENCES "projects" ("id") ON UPDATE NO ACTION ON DELETE CASCADE,
  CONSTRAINT "mcp_research_reports_request_id_fkey" FOREIGN KEY ("mcp_approval_request_id", "organization_id") REFERENCES "mcp_approval_requests" ("id", "organization_id") ON UPDATE NO ACTION ON DELETE CASCADE,
  CONSTRAINT "mcp_research_reports_report_check" CHECK (jsonb_typeof(report) = 'object'::text)
);
-- Create index "mcp_research_reports_id_request_id_key" to table: "mcp_research_reports"
CREATE UNIQUE INDEX "mcp_research_reports_id_request_id_key" ON "mcp_research_reports" ("id", "mcp_approval_request_id");
-- Create index "mcp_research_reports_organization_id_idx" to table: "mcp_research_reports"
CREATE INDEX "mcp_research_reports_organization_id_idx" ON "mcp_research_reports" ("organization_id");
-- Create index "mcp_research_reports_project_id_idx" to table: "mcp_research_reports"
CREATE INDEX "mcp_research_reports_project_id_idx" ON "mcp_research_reports" ("project_id");
-- Create index "mcp_research_reports_request_id_created_at_idx" to table: "mcp_research_reports"
CREATE INDEX "mcp_research_reports_request_id_created_at_idx" ON "mcp_research_reports" ("mcp_approval_request_id", "created_at" DESC) WHERE (deleted IS FALSE);
-- Create index "mcp_research_reports_request_id_idx" to table: "mcp_research_reports"
CREATE INDEX "mcp_research_reports_request_id_idx" ON "mcp_research_reports" ("mcp_approval_request_id");
-- Set comment to table: "mcp_research_reports"
COMMENT ON TABLE "mcp_research_reports" IS 'Research-agent output for an approval request. Findings are gathered and cited, never adjudicated — the admin decides.';
-- Create "mcp_approval_decisions" table
CREATE TABLE "mcp_approval_decisions" (
  "id" uuid NOT NULL DEFAULT generate_uuidv7(),
  "organization_id" text NOT NULL,
  "project_id" uuid NULL,
  "mcp_approval_request_id" uuid NOT NULL,
  "decision" text NOT NULL,
  "decided_by" text NOT NULL,
  "rationale" text NULL,
  "evidence_snapshot" jsonb NOT NULL DEFAULT '{}',
  "evidence_version" integer NOT NULL,
  "mcp_research_report_id" uuid NULL,
  "granted_principal_urns" text[] NOT NULL DEFAULT ARRAY[]::text[],
  "decided_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "created_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "updated_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "deleted_at" timestamptz NULL,
  "deleted" boolean NOT NULL GENERATED ALWAYS AS (deleted_at IS NOT NULL) STORED,
  PRIMARY KEY ("id"),
  CONSTRAINT "mcp_approval_decisions_organization_id_fkey" FOREIGN KEY ("organization_id") REFERENCES "organization_metadata" ("id") ON UPDATE NO ACTION ON DELETE CASCADE,
  CONSTRAINT "mcp_approval_decisions_project_id_fkey" FOREIGN KEY ("project_id") REFERENCES "projects" ("id") ON UPDATE NO ACTION ON DELETE CASCADE,
  CONSTRAINT "mcp_approval_decisions_request_id_fkey" FOREIGN KEY ("mcp_approval_request_id", "organization_id") REFERENCES "mcp_approval_requests" ("id", "organization_id") ON UPDATE NO ACTION ON DELETE CASCADE,
  CONSTRAINT "mcp_approval_decisions_research_report_fkey" FOREIGN KEY ("mcp_research_report_id", "mcp_approval_request_id") REFERENCES "mcp_research_reports" ("id", "mcp_approval_request_id") ON UPDATE NO ACTION ON DELETE CASCADE,
  CONSTRAINT "mcp_approval_decisions_evidence_snapshot_check" CHECK (jsonb_typeof(evidence_snapshot) = 'object'::text)
);
-- Create index "mcp_approval_decisions_organization_id_idx" to table: "mcp_approval_decisions"
CREATE INDEX "mcp_approval_decisions_organization_id_idx" ON "mcp_approval_decisions" ("organization_id");
-- Create index "mcp_approval_decisions_project_id_idx" to table: "mcp_approval_decisions"
CREATE INDEX "mcp_approval_decisions_project_id_idx" ON "mcp_approval_decisions" ("project_id");
-- Create index "mcp_approval_decisions_request_id_decided_at_idx" to table: "mcp_approval_decisions"
CREATE INDEX "mcp_approval_decisions_request_id_decided_at_idx" ON "mcp_approval_decisions" ("mcp_approval_request_id", "decided_at" DESC) WHERE (deleted IS FALSE);
-- Create index "mcp_approval_decisions_request_id_idx" to table: "mcp_approval_decisions"
CREATE INDEX "mcp_approval_decisions_request_id_idx" ON "mcp_approval_decisions" ("mcp_approval_request_id");
-- Create index "mcp_approval_decisions_research_report_id_idx" to table: "mcp_approval_decisions"
CREATE INDEX "mcp_approval_decisions_research_report_id_idx" ON "mcp_approval_decisions" ("mcp_research_report_id");
-- Set comment to table: "mcp_approval_decisions"
COMMENT ON TABLE "mcp_approval_decisions" IS 'Append-only approve/deny history with the rationale and the evidence it rested on.';
-- Set comment to column: "granted_principal_urns" on table: "mcp_approval_decisions"
COMMENT ON COLUMN "mcp_approval_decisions"."granted_principal_urns" IS 'Resolved blast radius of the approval. Empty for a denial.';
-- Create "mcp_approval_request_requesters" table
CREATE TABLE "mcp_approval_request_requesters" (
  "id" uuid NOT NULL DEFAULT generate_uuidv7(),
  "organization_id" text NOT NULL,
  "project_id" uuid NULL,
  "mcp_approval_request_id" uuid NOT NULL,
  "user_id" text NOT NULL,
  "user_email" text NULL,
  "note" text NULL,
  "requested_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "created_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "updated_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "deleted_at" timestamptz NULL,
  "deleted" boolean NOT NULL GENERATED ALWAYS AS (deleted_at IS NOT NULL) STORED,
  PRIMARY KEY ("id"),
  CONSTRAINT "mcp_approval_request_requesters_organization_id_fkey" FOREIGN KEY ("organization_id") REFERENCES "organization_metadata" ("id") ON UPDATE NO ACTION ON DELETE CASCADE,
  CONSTRAINT "mcp_approval_request_requesters_project_id_fkey" FOREIGN KEY ("project_id") REFERENCES "projects" ("id") ON UPDATE NO ACTION ON DELETE CASCADE,
  CONSTRAINT "mcp_approval_request_requesters_request_id_fkey" FOREIGN KEY ("mcp_approval_request_id", "organization_id") REFERENCES "mcp_approval_requests" ("id", "organization_id") ON UPDATE NO ACTION ON DELETE CASCADE
);
-- Create index "mcp_approval_request_requesters_organization_id_idx" to table: "mcp_approval_request_requesters"
CREATE INDEX "mcp_approval_request_requesters_organization_id_idx" ON "mcp_approval_request_requesters" ("organization_id");
-- Create index "mcp_approval_request_requesters_project_id_idx" to table: "mcp_approval_request_requesters"
CREATE INDEX "mcp_approval_request_requesters_project_id_idx" ON "mcp_approval_request_requesters" ("project_id");
-- Create index "mcp_approval_request_requesters_request_id_idx" to table: "mcp_approval_request_requesters"
CREATE INDEX "mcp_approval_request_requesters_request_id_idx" ON "mcp_approval_request_requesters" ("mcp_approval_request_id");
-- Create index "mcp_approval_request_requesters_request_id_user_id_key" to table: "mcp_approval_request_requesters"
CREATE UNIQUE INDEX "mcp_approval_request_requesters_request_id_user_id_key" ON "mcp_approval_request_requesters" ("mcp_approval_request_id", "user_id") WHERE (deleted IS FALSE);
-- Set comment to table: "mcp_approval_request_requesters"
COMMENT ON TABLE "mcp_approval_request_requesters" IS 'Who asked for a server and why. Separate from the request so demand is visible without duplicating reviews.';
