-- Create "risk_policy_challenges" table
CREATE TABLE "risk_policy_challenges" (
  "id" uuid NOT NULL DEFAULT generate_uuidv7(),
  "organization_id" text NOT NULL,
  "project_id" uuid NOT NULL,
  "risk_policy_id" uuid NOT NULL,
  "user_id" text NOT NULL,
  "tool_name" text NULL,
  "status" text NOT NULL DEFAULT 'challenged',
  "policy_name" text NULL,
  "entity" text NULL,
  "rule_id" text NULL,
  "challenged_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "acknowledged_at" timestamptz NULL,
  "expires_at" timestamptz NULL,
  "created_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "updated_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "deleted_at" timestamptz NULL,
  "deleted" boolean NOT NULL GENERATED ALWAYS AS (deleted_at IS NOT NULL) STORED,
  PRIMARY KEY ("id"),
  CONSTRAINT "risk_policy_challenges_organization_id_fkey" FOREIGN KEY ("organization_id") REFERENCES "organization_metadata" ("id") ON UPDATE NO ACTION ON DELETE CASCADE,
  CONSTRAINT "risk_policy_challenges_project_id_fkey" FOREIGN KEY ("project_id") REFERENCES "projects" ("id") ON UPDATE NO ACTION ON DELETE CASCADE,
  CONSTRAINT "risk_policy_challenges_risk_policy_id_fkey" FOREIGN KEY ("risk_policy_id") REFERENCES "risk_policies" ("id") ON UPDATE NO ACTION ON DELETE CASCADE,
  CONSTRAINT "risk_policy_challenges_status_check" CHECK (status = ANY (ARRAY['challenged'::text, 'acknowledged'::text, 'declined'::text]))
);
-- Create index "risk_policy_challenges_active_ack_idx" to table: "risk_policy_challenges"
CREATE INDEX "risk_policy_challenges_active_ack_idx" ON "risk_policy_challenges" ("project_id", "user_id", "risk_policy_id", "tool_name", "expires_at") WHERE ((deleted IS FALSE) AND (status = 'acknowledged'::text));
-- Create index "risk_policy_challenges_current_key" to table: "risk_policy_challenges"
CREATE UNIQUE INDEX "risk_policy_challenges_current_key" ON "risk_policy_challenges" ("project_id", "user_id", "risk_policy_id", "tool_name") NULLS NOT DISTINCT WHERE (deleted IS FALSE);
-- Create index "risk_policy_challenges_project_status_updated_idx" to table: "risk_policy_challenges"
CREATE INDEX "risk_policy_challenges_project_status_updated_idx" ON "risk_policy_challenges" ("project_id", "status", "updated_at" DESC) WHERE (deleted IS FALSE);
-- Set comment to table: "risk_policy_challenges"
COMMENT ON TABLE "risk_policy_challenges" IS 'Interactive warn/challenge lifecycle for warn-action policies: a warn match records a challenged row; the user self-service acknowledges to proceed on retry. Never stores the raw matched value.';
