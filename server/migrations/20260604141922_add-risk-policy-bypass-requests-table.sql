-- Create "risk_policy_bypass_requests" table
CREATE TABLE "risk_policy_bypass_requests" (
  "id" uuid NOT NULL DEFAULT generate_uuidv7(),
  "organization_id" text NOT NULL,
  "project_id" uuid NOT NULL,
  "risk_policy_id" uuid NOT NULL,
  "target_kind" text NULL,
  "target_label" text NULL,
  "target_key" text NULL,
  "target_dimensions" jsonb NOT NULL DEFAULT '{}',
  "requester_user_id" text NOT NULL,
  "requester_email" text NULL,
  "note" text NULL,
  "status" text NOT NULL DEFAULT 'requested',
  "decided_by" text NULL,
  "granted_principal_urns" text[] NOT NULL DEFAULT ARRAY[]::text[],
  "decided_at" timestamptz NULL,
  "created_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "updated_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "deleted_at" timestamptz NULL,
  "deleted" boolean NOT NULL GENERATED ALWAYS AS (deleted_at IS NOT NULL) STORED,
  PRIMARY KEY ("id"),
  CONSTRAINT "risk_policy_bypass_requests_organization_id_fkey" FOREIGN KEY ("organization_id") REFERENCES "organization_metadata" ("id") ON UPDATE NO ACTION ON DELETE CASCADE,
  CONSTRAINT "risk_policy_bypass_requests_project_id_fkey" FOREIGN KEY ("project_id") REFERENCES "projects" ("id") ON UPDATE NO ACTION ON DELETE CASCADE,
  CONSTRAINT "risk_policy_bypass_requests_risk_policy_id_fkey" FOREIGN KEY ("risk_policy_id") REFERENCES "risk_policies" ("id") ON UPDATE NO ACTION ON DELETE CASCADE,
  CONSTRAINT "risk_policy_bypass_requests_target_dimensions_check" CHECK (jsonb_typeof(target_dimensions) = 'object'::text)
);
-- Create index "risk_policy_bypass_requests_current_key" to table: "risk_policy_bypass_requests"
CREATE UNIQUE INDEX "risk_policy_bypass_requests_current_key" ON "risk_policy_bypass_requests" ("project_id", "requester_user_id", "risk_policy_id", "target_kind", "target_key") NULLS NOT DISTINCT WHERE (deleted IS FALSE);
-- Create index "risk_policy_bypass_requests_project_status_updated_idx" to table: "risk_policy_bypass_requests"
CREATE INDEX "risk_policy_bypass_requests_project_status_updated_idx" ON "risk_policy_bypass_requests" ("project_id", "status", "updated_at" DESC) WHERE (deleted IS FALSE);
-- Set comment to table: "risk_policy_bypass_requests"
COMMENT ON TABLE "risk_policy_bypass_requests" IS 'Risk-policy bypass request workflow. A block records a request here; an admin approves by granting risk_policy:bypass.';
-- Set comment to column: "target_kind" on table: "risk_policy_bypass_requests"
COMMENT ON COLUMN "risk_policy_bypass_requests"."target_kind" IS 'Generic target namespace for the bypass request, such as server_url. Empty means the whole policy.';
-- Set comment to column: "target_key" on table: "risk_policy_bypass_requests"
COMMENT ON COLUMN "risk_policy_bypass_requests"."target_key" IS 'Stable canonical key for deduplicating bypass requests within the target namespace.';
-- Set comment to column: "target_dimensions" on table: "risk_policy_bypass_requests"
COMMENT ON COLUMN "risk_policy_bypass_requests"."target_dimensions" IS 'Selector dimensions for the target, such as {"server_url":"mcp.example.com"}.';
