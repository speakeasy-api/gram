-- atlas:txmode none

-- Create index "api_keys_project_id_id_key" to table: "api_keys"
CREATE UNIQUE INDEX CONCURRENTLY "api_keys_project_id_id_key" ON "api_keys" ("project_id", "id");
-- Modify "assistants" table
ALTER TABLE "assistants" ADD COLUMN "runtime_version" smallint NOT NULL DEFAULT 2;
-- Set comment to column: "runtime_version" on table: "assistants"
COMMENT ON COLUMN "assistants"."runtime_version" IS '@access: confidential';
-- Drop index "assistant_runtimes_v2_one_per_assistant" from table: "assistant_runtimes"
DROP INDEX CONCURRENTLY "assistant_runtimes_v2_one_per_assistant";
-- Create index "assistant_runtimes_v2_one_per_assistant" to table: "assistant_runtimes"
CREATE UNIQUE INDEX CONCURRENTLY "assistant_runtimes_v2_one_per_assistant" ON "assistant_runtimes" ("project_id", "assistant_id") WHERE ((runtime_version = ANY (ARRAY[2, 3])) AND (deleted IS FALSE) AND (ended IS FALSE));
-- Create index "assistant_runtimes_project_id_id_key" to table: "assistant_runtimes"
CREATE UNIQUE INDEX CONCURRENTLY "assistant_runtimes_project_id_id_key" ON "assistant_runtimes" ("project_id", "id");
-- Create "assistant_inference_credentials" table
CREATE TABLE "assistant_inference_credentials" (
  "id" uuid NOT NULL DEFAULT generate_uuidv7(),
  "project_id" uuid NOT NULL,
  "assistant_id" uuid NOT NULL,
  "runtime_id" uuid NOT NULL,
  "api_key_id" uuid NOT NULL,
  "identity_json" jsonb NOT NULL,
  "created_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "revoked_at" timestamptz NULL,
  PRIMARY KEY ("id"),
  CONSTRAINT "assistant_inference_credentials_api_key_id_key" UNIQUE ("api_key_id"),
  CONSTRAINT "assistant_inference_credentials_api_key_id_fkey" FOREIGN KEY ("project_id", "api_key_id") REFERENCES "api_keys" ("project_id", "id") ON UPDATE NO ACTION ON DELETE CASCADE,
  CONSTRAINT "assistant_inference_credentials_assistant_id_fkey" FOREIGN KEY ("project_id", "assistant_id") REFERENCES "assistants" ("project_id", "id") ON UPDATE NO ACTION ON DELETE CASCADE,
  CONSTRAINT "assistant_inference_credentials_project_id_fkey" FOREIGN KEY ("project_id") REFERENCES "projects" ("id") ON UPDATE NO ACTION ON DELETE CASCADE,
  CONSTRAINT "assistant_inference_credentials_runtime_id_fkey" FOREIGN KEY ("project_id", "runtime_id") REFERENCES "assistant_runtimes" ("project_id", "id") ON UPDATE NO ACTION ON DELETE CASCADE
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
-- Set comment to column: "api_key_id" on table: "assistant_inference_credentials"
COMMENT ON COLUMN "assistant_inference_credentials"."api_key_id" IS '@access: confidential';
-- Set comment to column: "identity_json" on table: "assistant_inference_credentials"
COMMENT ON COLUMN "assistant_inference_credentials"."identity_json" IS '@access: opaque-restricted';
-- Set comment to column: "created_at" on table: "assistant_inference_credentials"
COMMENT ON COLUMN "assistant_inference_credentials"."created_at" IS '@access: confidential';
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
  CONSTRAINT "assistant_kit_gateway_credentials_assistant_id_fkey" FOREIGN KEY ("project_id", "assistant_id") REFERENCES "assistants" ("project_id", "id") ON UPDATE NO ACTION ON DELETE SET NULL,
  CONSTRAINT "assistant_kit_gateway_credentials_project_id_fkey" FOREIGN KEY ("project_id") REFERENCES "projects" ("id") ON UPDATE NO ACTION ON DELETE SET NULL,
  CONSTRAINT "assistant_kit_gateway_credentials_runtime_id_fkey" FOREIGN KEY ("project_id", "runtime_id") REFERENCES "assistant_runtimes" ("project_id", "id") ON UPDATE NO ACTION ON DELETE SET NULL,
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
-- Create index "assistant_threads_project_id_id_key" to table: "assistant_threads"
CREATE UNIQUE INDEX CONCURRENTLY "assistant_threads_project_id_id_key" ON "assistant_threads" ("project_id", "id");
-- Create index "assistant_thread_events_project_id_id_key" to table: "assistant_thread_events"
CREATE UNIQUE INDEX CONCURRENTLY "assistant_thread_events_project_id_id_key" ON "assistant_thread_events" ("project_id", "id");
-- Create "assistant_kit_interrupt_requests" table
CREATE TABLE "assistant_kit_interrupt_requests" (
  "id" uuid NOT NULL DEFAULT generate_uuidv7(),
  "project_id" uuid NULL,
  "assistant_id" uuid NULL,
  "runtime_id" uuid NULL,
  "assistant_thread_id" uuid NULL,
  "event_id" uuid NULL,
  "attempt" bigint NOT NULL,
  "outcome" text NOT NULL DEFAULT 'pending',
  "created_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "acknowledged_at" timestamptz NULL,
  PRIMARY KEY ("id"),
  CONSTRAINT "assistant_kit_interrupt_requests_assistant_id_fkey" FOREIGN KEY ("project_id", "assistant_id") REFERENCES "assistants" ("project_id", "id") ON UPDATE NO ACTION ON DELETE SET NULL,
  CONSTRAINT "assistant_kit_interrupt_requests_assistant_thread_id_fkey" FOREIGN KEY ("project_id", "assistant_thread_id") REFERENCES "assistant_threads" ("project_id", "id") ON UPDATE NO ACTION ON DELETE SET NULL,
  CONSTRAINT "assistant_kit_interrupt_requests_event_id_fkey" FOREIGN KEY ("project_id", "event_id") REFERENCES "assistant_thread_events" ("project_id", "id") ON UPDATE NO ACTION ON DELETE SET NULL,
  CONSTRAINT "assistant_kit_interrupt_requests_project_id_fkey" FOREIGN KEY ("project_id") REFERENCES "projects" ("id") ON UPDATE NO ACTION ON DELETE SET NULL,
  CONSTRAINT "assistant_kit_interrupt_requests_runtime_id_fkey" FOREIGN KEY ("project_id", "runtime_id") REFERENCES "assistant_runtimes" ("project_id", "id") ON UPDATE NO ACTION ON DELETE SET NULL
);
-- Create index "assistant_kit_interrupt_requests_attempt_key" to table: "assistant_kit_interrupt_requests"
CREATE UNIQUE INDEX "assistant_kit_interrupt_requests_attempt_key" ON "assistant_kit_interrupt_requests" ("project_id", "runtime_id", "event_id", "attempt");
-- Set comment to column: "id" on table: "assistant_kit_interrupt_requests"
COMMENT ON COLUMN "assistant_kit_interrupt_requests"."id" IS '@access: confidential';
-- Set comment to column: "project_id" on table: "assistant_kit_interrupt_requests"
COMMENT ON COLUMN "assistant_kit_interrupt_requests"."project_id" IS '@access: confidential';
-- Set comment to column: "assistant_id" on table: "assistant_kit_interrupt_requests"
COMMENT ON COLUMN "assistant_kit_interrupt_requests"."assistant_id" IS '@access: confidential';
-- Set comment to column: "runtime_id" on table: "assistant_kit_interrupt_requests"
COMMENT ON COLUMN "assistant_kit_interrupt_requests"."runtime_id" IS '@access: confidential';
-- Set comment to column: "assistant_thread_id" on table: "assistant_kit_interrupt_requests"
COMMENT ON COLUMN "assistant_kit_interrupt_requests"."assistant_thread_id" IS '@access: confidential';
-- Set comment to column: "event_id" on table: "assistant_kit_interrupt_requests"
COMMENT ON COLUMN "assistant_kit_interrupt_requests"."event_id" IS '@access: confidential';
-- Set comment to column: "attempt" on table: "assistant_kit_interrupt_requests"
COMMENT ON COLUMN "assistant_kit_interrupt_requests"."attempt" IS '@access: confidential';
-- Set comment to column: "outcome" on table: "assistant_kit_interrupt_requests"
COMMENT ON COLUMN "assistant_kit_interrupt_requests"."outcome" IS '@access: confidential';
-- Set comment to column: "created_at" on table: "assistant_kit_interrupt_requests"
COMMENT ON COLUMN "assistant_kit_interrupt_requests"."created_at" IS '@access: confidential';
-- Set comment to column: "acknowledged_at" on table: "assistant_kit_interrupt_requests"
COMMENT ON COLUMN "assistant_kit_interrupt_requests"."acknowledged_at" IS '@access: confidential';
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
  CONSTRAINT "assistant_kit_sessions_assistant_id_fkey" FOREIGN KEY ("project_id", "assistant_id") REFERENCES "assistants" ("project_id", "id") ON UPDATE NO ACTION ON DELETE SET NULL,
  CONSTRAINT "assistant_kit_sessions_assistant_thread_id_fkey" FOREIGN KEY ("project_id", "assistant_thread_id") REFERENCES "assistant_threads" ("project_id", "id") ON UPDATE NO ACTION ON DELETE SET NULL,
  CONSTRAINT "assistant_kit_sessions_project_id_fkey" FOREIGN KEY ("project_id") REFERENCES "projects" ("id") ON UPDATE NO ACTION ON DELETE SET NULL,
  CONSTRAINT "assistant_kit_sessions_runtime_id_fkey" FOREIGN KEY ("project_id", "runtime_id") REFERENCES "assistant_runtimes" ("project_id", "id") ON UPDATE NO ACTION ON DELETE SET NULL,
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
-- Create index "trigger_workload_bindings_project_id_id_key" to table: "trigger_workload_bindings"
CREATE UNIQUE INDEX CONCURRENTLY "trigger_workload_bindings_project_id_id_key" ON "trigger_workload_bindings" ("project_id", "id");
-- Modify "user_session_clients" table
ALTER TABLE "user_session_clients" ADD COLUMN "assistant_trigger_workload_binding_id" uuid NULL, ADD COLUMN "assistant_runtime_id" uuid NULL, ADD CONSTRAINT "user_session_clients_assistant_runtime_fkey" FOREIGN KEY ("assistant_runtime_id") REFERENCES "assistant_runtimes" ("id") ON UPDATE NO ACTION ON DELETE SET NULL NOT VALID, ADD CONSTRAINT "user_session_clients_assistant_runtime_tenant_fkey" FOREIGN KEY ("project_id", "assistant_runtime_id") REFERENCES "assistant_runtimes" ("project_id", "id") ON UPDATE NO ACTION ON DELETE NO ACTION NOT VALID, ADD CONSTRAINT "user_session_clients_assistant_workload_binding_fkey" FOREIGN KEY ("assistant_trigger_workload_binding_id") REFERENCES "trigger_workload_bindings" ("id") ON UPDATE NO ACTION ON DELETE SET NULL NOT VALID, ADD CONSTRAINT "user_session_clients_assistant_workload_binding_tenant_fkey" FOREIGN KEY ("project_id", "assistant_trigger_workload_binding_id") REFERENCES "trigger_workload_bindings" ("project_id", "id") ON UPDATE NO ACTION ON DELETE NO ACTION NOT VALID;
ALTER TABLE "user_session_clients" VALIDATE CONSTRAINT "user_session_clients_assistant_runtime_fkey";
ALTER TABLE "user_session_clients" VALIDATE CONSTRAINT "user_session_clients_assistant_runtime_tenant_fkey";
ALTER TABLE "user_session_clients" VALIDATE CONSTRAINT "user_session_clients_assistant_workload_binding_fkey";
ALTER TABLE "user_session_clients" VALIDATE CONSTRAINT "user_session_clients_assistant_workload_binding_tenant_fkey";
-- Create index "user_session_clients_assistant_runtime_idx" to table: "user_session_clients"
CREATE INDEX CONCURRENTLY "user_session_clients_assistant_runtime_idx" ON "user_session_clients" ("assistant_runtime_id") WHERE ((assistant_runtime_id IS NOT NULL) AND (deleted IS FALSE));
-- Create index "user_session_clients_assistant_workload_binding_idx" to table: "user_session_clients"
CREATE INDEX CONCURRENTLY "user_session_clients_assistant_workload_binding_idx" ON "user_session_clients" ("assistant_trigger_workload_binding_id") WHERE ((assistant_trigger_workload_binding_id IS NOT NULL) AND (deleted IS FALSE));
-- Set comment to column: "assistant_trigger_workload_binding_id" on table: "user_session_clients"
COMMENT ON COLUMN "user_session_clients"."assistant_trigger_workload_binding_id" IS '@access: confidential';
-- Set comment to column: "assistant_runtime_id" on table: "user_session_clients"
COMMENT ON COLUMN "user_session_clients"."assistant_runtime_id" IS '@access: confidential';
