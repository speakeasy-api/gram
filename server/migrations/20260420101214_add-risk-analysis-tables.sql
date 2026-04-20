-- Create "risk_policies" table
CREATE TABLE "risk_policies" (
  "id" uuid NOT NULL,
  "project_id" uuid NOT NULL,
  "organization_id" text NOT NULL,
  "enabled" boolean NOT NULL DEFAULT true,
  "name" text NOT NULL,
  "sources" text[] NOT NULL,
  "version" bigint NOT NULL,
  "created_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "updated_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "deleted_at" timestamptz NULL,
  PRIMARY KEY ("id")
);
-- Create index "risk_policies_project_id_idx" to table: "risk_policies"
CREATE INDEX "risk_policies_project_id_idx" ON "risk_policies" ("project_id") WHERE (deleted_at IS NULL);
-- Create "risk_results" table
CREATE TABLE "risk_results" (
  "id" uuid NOT NULL,
  "project_id" uuid NOT NULL,
  "organization_id" text NOT NULL,
  "risk_policy_id" uuid NOT NULL,
  "risk_policy_version" bigint NOT NULL,
  "chat_message_id" uuid NOT NULL,
  "source" text NOT NULL,
  "found" boolean NOT NULL,
  "rule_id" text NULL,
  "description" text NULL,
  "match" text NULL,
  "start_pos" integer NULL,
  "end_pos" integer NULL,
  "confidence" double precision NULL,
  "tags" text[] NULL,
  "created_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  PRIMARY KEY ("id")
);
-- Create index "risk_results_chat_message_id_idx" to table: "risk_results"
CREATE INDEX "risk_results_chat_message_id_idx" ON "risk_results" ("chat_message_id");
-- Create index "risk_results_policy_version_message_idx" to table: "risk_results"
CREATE INDEX "risk_results_policy_version_message_idx" ON "risk_results" ("risk_policy_id", "risk_policy_version", "chat_message_id");
-- Create index "risk_results_project_id_risk_policy_id_idx" to table: "risk_results"
CREATE INDEX "risk_results_project_id_risk_policy_id_idx" ON "risk_results" ("project_id", "risk_policy_id");
