-- atlas:txmode none

-- Drop index "assistant_runtimes_v2_one_per_assistant" from table: "assistant_runtimes"
DROP INDEX CONCURRENTLY "assistant_runtimes_v2_one_per_assistant";
-- Create index "assistant_runtimes_v2_one_per_assistant" to table: "assistant_runtimes"
CREATE UNIQUE INDEX CONCURRENTLY "assistant_runtimes_v2_one_per_assistant" ON "assistant_runtimes" ("project_id", "assistant_id") WHERE ((runtime_version = ANY (ARRAY[2, 3])) AND (deleted IS FALSE) AND (ended IS FALSE));
-- Create "assistant_inference_credentials" table
CREATE TABLE "assistant_inference_credentials" (
  "id" uuid NOT NULL DEFAULT generate_uuidv7(),
  "project_id" uuid NOT NULL,
  "assistant_id" uuid NOT NULL,
  "runtime_id" uuid NOT NULL,
  "token_hash" bytea NOT NULL,
  "identity_json" jsonb NOT NULL,
  "policy_json" jsonb NOT NULL,
  "policy_version" integer NOT NULL,
  "created_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "expires_at" timestamptz NOT NULL,
  "revoked_at" timestamptz NULL,
  PRIMARY KEY ("id"),
  CONSTRAINT "assistant_inference_credentials_token_hash_key" UNIQUE ("token_hash"),
  CONSTRAINT "assistant_inference_credentials_assistant_id_fkey" FOREIGN KEY ("assistant_id") REFERENCES "assistants" ("id") ON UPDATE NO ACTION ON DELETE CASCADE,
  CONSTRAINT "assistant_inference_credentials_project_id_fkey" FOREIGN KEY ("project_id") REFERENCES "projects" ("id") ON UPDATE NO ACTION ON DELETE CASCADE,
  CONSTRAINT "assistant_inference_credentials_runtime_id_fkey" FOREIGN KEY ("runtime_id") REFERENCES "assistant_runtimes" ("id") ON UPDATE NO ACTION ON DELETE CASCADE,
  CONSTRAINT "assistant_inference_credentials_check" CHECK ((expires_at > created_at) AND (expires_at <= (created_at + '30 days'::interval))),
  CONSTRAINT "assistant_inference_credentials_policy_version_check" CHECK (policy_version > 0),
  CONSTRAINT "assistant_inference_credentials_token_hash_check" CHECK (octet_length(token_hash) = 32)
);
-- Create index "assistant_inference_credentials_runtime_idx" to table: "assistant_inference_credentials"
CREATE INDEX "assistant_inference_credentials_runtime_idx" ON "assistant_inference_credentials" ("project_id", "runtime_id");
-- Set comment to column: "id" on table: "assistant_inference_credentials"
COMMENT ON COLUMN "assistant_inference_credentials"."id" IS '@access: confidential';
-- Set comment to column: "project_id" on table: "assistant_inference_credentials"
COMMENT ON COLUMN "assistant_inference_credentials"."project_id" IS '@access: confidential';
-- Set comment to column: "assistant_id" on table: "assistant_inference_credentials"
COMMENT ON COLUMN "assistant_inference_credentials"."assistant_id" IS '@access: confidential';
-- Set comment to column: "runtime_id" on table: "assistant_inference_credentials"
COMMENT ON COLUMN "assistant_inference_credentials"."runtime_id" IS '@access: confidential';
-- Set comment to column: "token_hash" on table: "assistant_inference_credentials"
COMMENT ON COLUMN "assistant_inference_credentials"."token_hash" IS '@access: opaque-restricted';
-- Set comment to column: "identity_json" on table: "assistant_inference_credentials"
COMMENT ON COLUMN "assistant_inference_credentials"."identity_json" IS '@access: opaque-restricted';
-- Set comment to column: "policy_json" on table: "assistant_inference_credentials"
COMMENT ON COLUMN "assistant_inference_credentials"."policy_json" IS '@access: opaque-restricted';
-- Set comment to column: "policy_version" on table: "assistant_inference_credentials"
COMMENT ON COLUMN "assistant_inference_credentials"."policy_version" IS '@access: confidential';
-- Set comment to column: "created_at" on table: "assistant_inference_credentials"
COMMENT ON COLUMN "assistant_inference_credentials"."created_at" IS '@access: confidential';
-- Set comment to column: "expires_at" on table: "assistant_inference_credentials"
COMMENT ON COLUMN "assistant_inference_credentials"."expires_at" IS '@access: confidential';
-- Set comment to column: "revoked_at" on table: "assistant_inference_credentials"
COMMENT ON COLUMN "assistant_inference_credentials"."revoked_at" IS '@access: confidential';
-- Create "assistant_kit_gateway_credentials" table
CREATE TABLE "assistant_kit_gateway_credentials" (
  "id" uuid NOT NULL DEFAULT generate_uuidv7(),
  "project_id" uuid NULL,
  "assistant_id" uuid NULL,
  "runtime_id" uuid NULL,
  "token_ciphertext" text NOT NULL,
  "created_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "updated_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  PRIMARY KEY ("id"),
  CONSTRAINT "assistant_kit_gateway_credentials_assistant_id_fkey" FOREIGN KEY ("assistant_id") REFERENCES "assistants" ("id") ON UPDATE NO ACTION ON DELETE SET NULL,
  CONSTRAINT "assistant_kit_gateway_credentials_project_id_fkey" FOREIGN KEY ("project_id") REFERENCES "projects" ("id") ON UPDATE NO ACTION ON DELETE SET NULL,
  CONSTRAINT "assistant_kit_gateway_credentials_runtime_id_fkey" FOREIGN KEY ("runtime_id") REFERENCES "assistant_runtimes" ("id") ON UPDATE NO ACTION ON DELETE SET NULL,
  CONSTRAINT "assistant_kit_gateway_credentials_token_ciphertext_check" CHECK (token_ciphertext <> ''::text)
);
-- Create index "assistant_kit_gateway_credentials_runtime_key" to table: "assistant_kit_gateway_credentials"
CREATE UNIQUE INDEX "assistant_kit_gateway_credentials_runtime_key" ON "assistant_kit_gateway_credentials" ("runtime_id");
-- Set comment to column: "id" on table: "assistant_kit_gateway_credentials"
COMMENT ON COLUMN "assistant_kit_gateway_credentials"."id" IS '@access: confidential';
-- Set comment to column: "project_id" on table: "assistant_kit_gateway_credentials"
COMMENT ON COLUMN "assistant_kit_gateway_credentials"."project_id" IS '@access: confidential';
-- Set comment to column: "assistant_id" on table: "assistant_kit_gateway_credentials"
COMMENT ON COLUMN "assistant_kit_gateway_credentials"."assistant_id" IS '@access: confidential';
-- Set comment to column: "runtime_id" on table: "assistant_kit_gateway_credentials"
COMMENT ON COLUMN "assistant_kit_gateway_credentials"."runtime_id" IS '@access: confidential';
-- Set comment to column: "token_ciphertext" on table: "assistant_kit_gateway_credentials"
COMMENT ON COLUMN "assistant_kit_gateway_credentials"."token_ciphertext" IS '@access: secret-restricted';
-- Set comment to column: "created_at" on table: "assistant_kit_gateway_credentials"
COMMENT ON COLUMN "assistant_kit_gateway_credentials"."created_at" IS '@access: confidential';
-- Set comment to column: "updated_at" on table: "assistant_kit_gateway_credentials"
COMMENT ON COLUMN "assistant_kit_gateway_credentials"."updated_at" IS '@access: confidential';
-- Create "assistant_kit_sessions" table
CREATE TABLE "assistant_kit_sessions" (
  "id" uuid NOT NULL DEFAULT generate_uuidv7(),
  "project_id" uuid NULL,
  "assistant_id" uuid NULL,
  "runtime_id" uuid NULL,
  "assistant_thread_id" uuid NULL,
  "acp_session_id" text NOT NULL,
  "created_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  PRIMARY KEY ("id"),
  CONSTRAINT "assistant_kit_sessions_assistant_id_fkey" FOREIGN KEY ("assistant_id") REFERENCES "assistants" ("id") ON UPDATE NO ACTION ON DELETE SET NULL,
  CONSTRAINT "assistant_kit_sessions_assistant_thread_id_fkey" FOREIGN KEY ("assistant_thread_id") REFERENCES "assistant_threads" ("id") ON UPDATE NO ACTION ON DELETE SET NULL,
  CONSTRAINT "assistant_kit_sessions_project_id_fkey" FOREIGN KEY ("project_id") REFERENCES "projects" ("id") ON UPDATE NO ACTION ON DELETE SET NULL,
  CONSTRAINT "assistant_kit_sessions_runtime_id_fkey" FOREIGN KEY ("runtime_id") REFERENCES "assistant_runtimes" ("id") ON UPDATE NO ACTION ON DELETE SET NULL,
  CONSTRAINT "assistant_kit_sessions_acp_session_id_check" CHECK (acp_session_id <> ''::text)
);
-- Create index "assistant_kit_sessions_runtime_session_key" to table: "assistant_kit_sessions"
CREATE UNIQUE INDEX "assistant_kit_sessions_runtime_session_key" ON "assistant_kit_sessions" ("runtime_id", "acp_session_id");
-- Create index "assistant_kit_sessions_runtime_thread_key" to table: "assistant_kit_sessions"
CREATE UNIQUE INDEX "assistant_kit_sessions_runtime_thread_key" ON "assistant_kit_sessions" ("runtime_id", "assistant_thread_id");
-- Set comment to column: "id" on table: "assistant_kit_sessions"
COMMENT ON COLUMN "assistant_kit_sessions"."id" IS '@access: confidential';
-- Set comment to column: "project_id" on table: "assistant_kit_sessions"
COMMENT ON COLUMN "assistant_kit_sessions"."project_id" IS '@access: confidential';
-- Set comment to column: "assistant_id" on table: "assistant_kit_sessions"
COMMENT ON COLUMN "assistant_kit_sessions"."assistant_id" IS '@access: confidential';
-- Set comment to column: "runtime_id" on table: "assistant_kit_sessions"
COMMENT ON COLUMN "assistant_kit_sessions"."runtime_id" IS '@access: confidential';
-- Set comment to column: "assistant_thread_id" on table: "assistant_kit_sessions"
COMMENT ON COLUMN "assistant_kit_sessions"."assistant_thread_id" IS '@access: confidential';
-- Set comment to column: "acp_session_id" on table: "assistant_kit_sessions"
COMMENT ON COLUMN "assistant_kit_sessions"."acp_session_id" IS '@access: confidential';
-- Set comment to column: "created_at" on table: "assistant_kit_sessions"
COMMENT ON COLUMN "assistant_kit_sessions"."created_at" IS '@access: confidential';
-- Modify "user_session_clients" table
ALTER TABLE "user_session_clients" ADD COLUMN "assistant_trigger_workload_binding_id" uuid NULL, ADD COLUMN "assistant_runtime_id" uuid NULL, ADD CONSTRAINT "user_session_clients_assistant_runtime_fkey" FOREIGN KEY ("assistant_runtime_id") REFERENCES "assistant_runtimes" ("id") ON UPDATE NO ACTION ON DELETE SET NULL, ADD CONSTRAINT "user_session_clients_assistant_workload_binding_fkey" FOREIGN KEY ("assistant_trigger_workload_binding_id") REFERENCES "trigger_workload_bindings" ("id") ON UPDATE NO ACTION ON DELETE SET NULL;
-- Create index "user_session_clients_assistant_runtime_idx" to table: "user_session_clients"
CREATE INDEX CONCURRENTLY "user_session_clients_assistant_runtime_idx" ON "user_session_clients" ("assistant_runtime_id") WHERE ((assistant_runtime_id IS NOT NULL) AND (deleted IS FALSE));
-- Create index "user_session_clients_assistant_workload_binding_idx" to table: "user_session_clients"
CREATE INDEX CONCURRENTLY "user_session_clients_assistant_workload_binding_idx" ON "user_session_clients" ("assistant_trigger_workload_binding_id") WHERE ((assistant_trigger_workload_binding_id IS NOT NULL) AND (deleted IS FALSE));
-- Set comment to column: "assistant_trigger_workload_binding_id" on table: "user_session_clients"
COMMENT ON COLUMN "user_session_clients"."assistant_trigger_workload_binding_id" IS '@access: confidential';
-- Set comment to column: "assistant_runtime_id" on table: "user_session_clients"
COMMENT ON COLUMN "user_session_clients"."assistant_runtime_id" IS '@access: confidential';
