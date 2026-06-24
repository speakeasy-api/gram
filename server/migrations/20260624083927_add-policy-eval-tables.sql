-- Create "policy_eval_runs" table
CREATE TABLE "policy_eval_runs" (
  "id" uuid NOT NULL DEFAULT generate_uuidv7(),
  "project_id" uuid NOT NULL,
  "organization_id" text NOT NULL,
  "risk_policy_id" uuid NULL,
  "risk_policy_version" bigint NULL,
  "config_snapshot" jsonb NULL,
  "sample_definition" jsonb NOT NULL,
  "status" text NOT NULL DEFAULT 'pending',
  "requested_by" text NULL,
  "messages_scanned" integer NOT NULL DEFAULT 0,
  "findings_count" integer NOT NULL DEFAULT 0,
  "total_cost_usd" double precision NOT NULL DEFAULT 0,
  "input_tokens" bigint NOT NULL DEFAULT 0,
  "output_tokens" bigint NOT NULL DEFAULT 0,
  "judge_latency_p50_ms" integer NULL,
  "judge_latency_p95_ms" integer NULL,
  "error" text NULL,
  "created_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "started_at" timestamptz NULL,
  "completed_at" timestamptz NULL,
  "expires_at" timestamptz NULL,
  PRIMARY KEY ("id"),
  CONSTRAINT "policy_eval_runs_organization_id_fkey" FOREIGN KEY ("organization_id") REFERENCES "organization_metadata" ("id") ON UPDATE NO ACTION ON DELETE CASCADE,
  CONSTRAINT "policy_eval_runs_project_id_fkey" FOREIGN KEY ("project_id") REFERENCES "projects" ("id") ON UPDATE NO ACTION ON DELETE CASCADE,
  CONSTRAINT "policy_eval_runs_risk_policy_id_fkey" FOREIGN KEY ("risk_policy_id") REFERENCES "risk_policies" ("id") ON UPDATE NO ACTION ON DELETE CASCADE,
  CONSTRAINT "policy_eval_runs_status_check" CHECK (status = ANY (ARRAY['pending'::text, 'running'::text, 'completed'::text, 'cancelled'::text, 'failed'::text]))
);
-- Create index "policy_eval_runs_expires_at_idx" to table: "policy_eval_runs"
CREATE INDEX "policy_eval_runs_expires_at_idx" ON "policy_eval_runs" ("expires_at") WHERE (expires_at IS NOT NULL);
-- Create index "policy_eval_runs_policy_idx" to table: "policy_eval_runs"
CREATE INDEX "policy_eval_runs_policy_idx" ON "policy_eval_runs" ("project_id", "risk_policy_id") WHERE (risk_policy_id IS NOT NULL);
-- Create index "policy_eval_runs_project_created_idx" to table: "policy_eval_runs"
CREATE INDEX "policy_eval_runs_project_created_idx" ON "policy_eval_runs" ("project_id", "created_at" DESC);
-- Create "policy_eval_findings" table
CREATE TABLE "policy_eval_findings" (
  "id" uuid NOT NULL DEFAULT generate_uuidv7(),
  "policy_eval_run_id" uuid NOT NULL,
  "project_id" uuid NOT NULL,
  "organization_id" text NOT NULL,
  "chat_message_id" uuid NOT NULL,
  "source" text NOT NULL,
  "rule_id" text NULL,
  "description" text NULL,
  "match" text NULL,
  "start_pos" integer NULL,
  "end_pos" integer NULL,
  "confidence" double precision NULL,
  "tags" text[] NULL,
  "spans" jsonb NULL,
  "created_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  PRIMARY KEY ("id"),
  CONSTRAINT "policy_eval_findings_chat_message_id_fkey" FOREIGN KEY ("chat_message_id") REFERENCES "chat_messages" ("id") ON UPDATE NO ACTION ON DELETE CASCADE,
  CONSTRAINT "policy_eval_findings_organization_id_fkey" FOREIGN KEY ("organization_id") REFERENCES "organization_metadata" ("id") ON UPDATE NO ACTION ON DELETE CASCADE,
  CONSTRAINT "policy_eval_findings_project_id_fkey" FOREIGN KEY ("project_id") REFERENCES "projects" ("id") ON UPDATE NO ACTION ON DELETE CASCADE,
  CONSTRAINT "policy_eval_findings_run_id_fkey" FOREIGN KEY ("policy_eval_run_id") REFERENCES "policy_eval_runs" ("id") ON UPDATE NO ACTION ON DELETE CASCADE
);
-- Create index "policy_eval_findings_project_run_idx" to table: "policy_eval_findings"
CREATE INDEX "policy_eval_findings_project_run_idx" ON "policy_eval_findings" ("project_id", "policy_eval_run_id");
-- Create index "policy_eval_findings_run_idx" to table: "policy_eval_findings"
CREATE INDEX "policy_eval_findings_run_idx" ON "policy_eval_findings" ("policy_eval_run_id", "created_at" DESC);
