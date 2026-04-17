-- Create "risk_policies" table
CREATE TABLE "risk_policies" (
  "id" uuid NOT NULL DEFAULT generate_uuidv7(),
  "project_id" uuid NOT NULL,
  "organization_id" text NOT NULL,
  "name" text NOT NULL,
  "sources" text[] NOT NULL DEFAULT '{gitleaks}',
  "enabled" boolean NOT NULL DEFAULT true,
  "version" bigint NOT NULL DEFAULT 1,
  "created_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "updated_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "deleted_at" timestamptz NULL,
  "deleted" boolean NOT NULL GENERATED ALWAYS AS (deleted_at IS NOT NULL) STORED,
  PRIMARY KEY ("id"),
  CONSTRAINT "risk_policies_project_id_fkey" FOREIGN KEY ("project_id") REFERENCES "projects" ("id") ON UPDATE NO ACTION ON DELETE SET NULL,
  CONSTRAINT "risk_policies_name_check" CHECK ((name <> ''::text) AND (char_length(name) <= 100))
);
-- Create index "risk_policies_project_id_idx" to table: "risk_policies"
CREATE INDEX "risk_policies_project_id_idx" ON "risk_policies" ("project_id") WHERE (deleted IS FALSE);
-- Create "risk_results" table
CREATE TABLE "risk_results" (
  "id" uuid NOT NULL DEFAULT generate_uuidv7(),
  "project_id" uuid NOT NULL,
  "risk_policy_id" uuid NOT NULL,
  "policy_version" bigint NOT NULL,
  "chat_message_id" uuid NOT NULL,
  "source" text NOT NULL,
  "found" boolean NOT NULL DEFAULT false,
  "rule_id" text NULL,
  "description" text NULL,
  "match" text NULL,
  "start_line" integer NULL,
  "start_column" integer NULL,
  "end_line" integer NULL,
  "end_column" integer NULL,
  "confidence" double precision NULL,
  "tags" text[] NULL,
  "created_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  PRIMARY KEY ("id"),
  CONSTRAINT "risk_results_chat_message_id_fkey" FOREIGN KEY ("chat_message_id") REFERENCES "chat_messages" ("id") ON UPDATE NO ACTION ON DELETE CASCADE,
  CONSTRAINT "risk_results_project_id_fkey" FOREIGN KEY ("project_id") REFERENCES "projects" ("id") ON UPDATE NO ACTION ON DELETE CASCADE,
  CONSTRAINT "risk_results_risk_policy_id_fkey" FOREIGN KEY ("risk_policy_id") REFERENCES "risk_policies" ("id") ON UPDATE NO ACTION ON DELETE CASCADE
);
-- Create index "risk_results_chat_message_id_idx" to table: "risk_results"
CREATE INDEX "risk_results_chat_message_id_idx" ON "risk_results" ("chat_message_id");
-- Create index "risk_results_project_id_risk_policy_id_idx" to table: "risk_results"
CREATE INDEX "risk_results_project_id_risk_policy_id_idx" ON "risk_results" ("project_id", "risk_policy_id");
