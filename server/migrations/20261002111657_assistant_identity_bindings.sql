-- atlas:txmode none

-- Modify "agents" table
ALTER TABLE "agents" ADD CONSTRAINT "agents_identity_epoch_check" CHECK (identity_epoch > 0), ADD COLUMN "identity_epoch" bigint NOT NULL DEFAULT 1;
-- Create index "agents_organization_project_id_key" to table: "agents"
CREATE UNIQUE INDEX CONCURRENTLY "agents_organization_project_id_key" ON "agents" ("organization_id", "project_id", "id");
-- Modify "assistants" table
ALTER TABLE "assistants" ADD CONSTRAINT "assistants_create_request_check" CHECK ((create_request_key IS NULL) = (create_request_hash IS NULL)), ADD COLUMN "create_request_key" text NULL, ADD COLUMN "create_request_hash" text NULL;
-- Create index "assistants_create_request_key" to table: "assistants"
CREATE UNIQUE INDEX CONCURRENTLY "assistants_create_request_key" ON "assistants" ("project_id", "create_request_key") WHERE (create_request_key IS NOT NULL);
-- Create index "assistants_organization_project_id_key" to table: "assistants"
CREATE UNIQUE INDEX CONCURRENTLY "assistants_organization_project_id_key" ON "assistants" ("organization_id", "project_id", "id");
-- Create "assistant_agent_bindings" table
CREATE TABLE "assistant_agent_bindings" (
  "id" uuid NOT NULL DEFAULT generate_uuidv7(),
  "organization_id" text NOT NULL,
  "project_id" uuid NOT NULL,
  "project_ref_organization_id" text NULL,
  "project_ref_id" uuid NULL,
  "original_assistant_id" uuid NOT NULL,
  "assistant_ref_organization_id" text NULL,
  "assistant_ref_project_id" uuid NULL,
  "assistant_id" uuid NULL,
  "original_agent_id" uuid NOT NULL,
  "agent_ref_organization_id" text NULL,
  "agent_ref_project_id" uuid NULL,
  "agent_id" uuid NULL,
  "generation" bigint NOT NULL,
  "created_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "updated_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "deleted_at" timestamptz NULL,
  "deleted" boolean NOT NULL GENERATED ALWAYS AS (deleted_at IS NOT NULL) STORED,
  PRIMARY KEY ("id"),
  CONSTRAINT "assistant_agent_bindings_agent_fkey" FOREIGN KEY ("agent_ref_organization_id", "agent_ref_project_id", "agent_id") REFERENCES "agents" ("organization_id", "project_id", "id") ON UPDATE NO ACTION ON DELETE SET NULL,
  CONSTRAINT "assistant_agent_bindings_assistant_fkey" FOREIGN KEY ("assistant_ref_organization_id", "assistant_ref_project_id", "assistant_id") REFERENCES "assistants" ("organization_id", "project_id", "id") ON UPDATE NO ACTION ON DELETE SET NULL,
  CONSTRAINT "assistant_agent_bindings_project_fkey" FOREIGN KEY ("project_ref_organization_id", "project_ref_id") REFERENCES "projects" ("organization_id", "id") ON UPDATE NO ACTION ON DELETE SET NULL,
  CONSTRAINT "assistant_agent_bindings_agent_ref_check" CHECK (((agent_ref_organization_id IS NULL) AND (agent_ref_project_id IS NULL) AND (agent_id IS NULL)) OR ((agent_ref_organization_id IS NOT NULL) AND (agent_ref_project_id IS NOT NULL) AND (agent_id IS NOT NULL) AND (agent_ref_organization_id = organization_id) AND (agent_ref_project_id = project_id) AND (agent_id = original_agent_id))),
  CONSTRAINT "assistant_agent_bindings_assistant_ref_check" CHECK (((assistant_ref_organization_id IS NULL) AND (assistant_ref_project_id IS NULL) AND (assistant_id IS NULL)) OR ((assistant_ref_organization_id IS NOT NULL) AND (assistant_ref_project_id IS NOT NULL) AND (assistant_id IS NOT NULL) AND (assistant_ref_organization_id = organization_id) AND (assistant_ref_project_id = project_id) AND (assistant_id = original_assistant_id))),
  CONSTRAINT "assistant_agent_bindings_generation_check" CHECK (generation > 0),
  CONSTRAINT "assistant_agent_bindings_project_ref_check" CHECK (((project_ref_organization_id IS NULL) AND (project_ref_id IS NULL)) OR ((project_ref_organization_id IS NOT NULL) AND (project_ref_id IS NOT NULL) AND (project_ref_organization_id = organization_id) AND (project_ref_id = project_id)))
);
-- Create index "assistant_agent_bindings_agent_ref_idx" to table: "assistant_agent_bindings"
CREATE INDEX "assistant_agent_bindings_agent_ref_idx" ON "assistant_agent_bindings" ("agent_ref_organization_id", "agent_ref_project_id", "agent_id");
-- Create index "assistant_agent_bindings_assistant_generation_key" to table: "assistant_agent_bindings"
CREATE UNIQUE INDEX "assistant_agent_bindings_assistant_generation_key" ON "assistant_agent_bindings" ("original_assistant_id", "generation");
-- Create index "assistant_agent_bindings_assistant_ref_idx" to table: "assistant_agent_bindings"
CREATE INDEX "assistant_agent_bindings_assistant_ref_idx" ON "assistant_agent_bindings" ("assistant_ref_organization_id", "assistant_ref_project_id", "assistant_id");
-- Create index "assistant_agent_bindings_live_agent_key" to table: "assistant_agent_bindings"
CREATE UNIQUE INDEX "assistant_agent_bindings_live_agent_key" ON "assistant_agent_bindings" ("original_agent_id") WHERE (deleted IS FALSE);
-- Create index "assistant_agent_bindings_live_assistant_key" to table: "assistant_agent_bindings"
CREATE UNIQUE INDEX "assistant_agent_bindings_live_assistant_key" ON "assistant_agent_bindings" ("original_assistant_id") WHERE (deleted IS FALSE);
-- Create index "assistant_agent_bindings_project_ref_idx" to table: "assistant_agent_bindings"
CREATE INDEX "assistant_agent_bindings_project_ref_idx" ON "assistant_agent_bindings" ("project_ref_organization_id", "project_ref_id");
-- Create index "assistant_agent_bindings_tenant_id_key" to table: "assistant_agent_bindings"
CREATE UNIQUE INDEX "assistant_agent_bindings_tenant_id_key" ON "assistant_agent_bindings" ("organization_id", "project_id", "id");
-- Modify "workload_issuers" table
ALTER TABLE "workload_issuers" ADD CONSTRAINT "workload_issuers_trust_source_check" CHECK ((issuer_kind <> 'system'::text) OR ((jwks_uri = ''::text) AND (project_id IS NOT NULL) AND (NOT allow_wildcard_admission))), ADD COLUMN "issuer_kind" text NOT NULL DEFAULT 'remote';
-- Create index "workload_issuers_system_project_key" to table: "workload_issuers"
CREATE UNIQUE INDEX CONCURRENTLY "workload_issuers_system_project_key" ON "workload_issuers" ("organization_id", "project_id") WHERE (issuer_kind = 'system'::text);
-- Create index "workload_issuers_tenant_kind_key" to table: "workload_issuers"
CREATE UNIQUE INDEX CONCURRENTLY "workload_issuers_tenant_kind_key" ON "workload_issuers" ("organization_id", "project_id", "id", "issuer_kind");
-- Create index "trigger_instances_organization_project_id_key" to table: "trigger_instances"
CREATE UNIQUE INDEX CONCURRENTLY "trigger_instances_organization_project_id_key" ON "trigger_instances" ("organization_id", "project_id", "id");
-- Create "trigger_workload_bindings" table
CREATE TABLE "trigger_workload_bindings" (
  "id" uuid NOT NULL DEFAULT generate_uuidv7(),
  "organization_id" text NOT NULL,
  "project_id" uuid NOT NULL,
  "project_ref_organization_id" text NULL,
  "project_ref_id" uuid NULL,
  "original_trigger_id" uuid NOT NULL,
  "trigger_ref_organization_id" text NULL,
  "trigger_ref_project_id" uuid NULL,
  "trigger_id" uuid NULL,
  "original_assistant_binding_id" uuid NOT NULL,
  "assistant_binding_ref_organization_id" text NULL,
  "assistant_binding_ref_project_id" uuid NULL,
  "assistant_binding_id" uuid NULL,
  "assistant_binding_generation" bigint NOT NULL,
  "original_workload_issuer_id" uuid NOT NULL,
  "workload_issuer_ref_organization_id" text NULL,
  "workload_issuer_ref_project_id" uuid NULL,
  "workload_issuer_id" uuid NULL,
  "workload_issuer_ref_kind" text NULL,
  "subject" text NOT NULL,
  "generation" bigint NOT NULL,
  "created_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "updated_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "deleted_at" timestamptz NULL,
  "deleted" boolean NOT NULL GENERATED ALWAYS AS (deleted_at IS NOT NULL) STORED,
  PRIMARY KEY ("id"),
  CONSTRAINT "trigger_workload_bindings_assistant_fkey" FOREIGN KEY ("assistant_binding_ref_organization_id", "assistant_binding_ref_project_id", "assistant_binding_id") REFERENCES "assistant_agent_bindings" ("organization_id", "project_id", "id") ON UPDATE NO ACTION ON DELETE SET NULL,
  CONSTRAINT "trigger_workload_bindings_issuer_fkey" FOREIGN KEY ("workload_issuer_ref_organization_id", "workload_issuer_ref_project_id", "workload_issuer_id", "workload_issuer_ref_kind") REFERENCES "workload_issuers" ("organization_id", "project_id", "id", "issuer_kind") ON UPDATE NO ACTION ON DELETE SET NULL,
  CONSTRAINT "trigger_workload_bindings_project_fkey" FOREIGN KEY ("project_ref_organization_id", "project_ref_id") REFERENCES "projects" ("organization_id", "id") ON UPDATE NO ACTION ON DELETE SET NULL,
  CONSTRAINT "trigger_workload_bindings_trigger_fkey" FOREIGN KEY ("trigger_ref_organization_id", "trigger_ref_project_id", "trigger_id") REFERENCES "trigger_instances" ("organization_id", "project_id", "id") ON UPDATE NO ACTION ON DELETE SET NULL,
  CONSTRAINT "trigger_workload_bindings_assistant_ref_check" CHECK (((assistant_binding_ref_organization_id IS NULL) AND (assistant_binding_ref_project_id IS NULL) AND (assistant_binding_id IS NULL)) OR ((assistant_binding_ref_organization_id IS NOT NULL) AND (assistant_binding_ref_project_id IS NOT NULL) AND (assistant_binding_id IS NOT NULL) AND (assistant_binding_ref_organization_id = organization_id) AND (assistant_binding_ref_project_id = project_id) AND (assistant_binding_id = original_assistant_binding_id))),
  CONSTRAINT "trigger_workload_bindings_generation_check" CHECK ((generation > 0) AND (assistant_binding_generation > 0)),
  CONSTRAINT "trigger_workload_bindings_issuer_ref_check" CHECK (((workload_issuer_ref_organization_id IS NULL) AND (workload_issuer_ref_project_id IS NULL) AND (workload_issuer_id IS NULL) AND (workload_issuer_ref_kind IS NULL)) OR ((workload_issuer_ref_organization_id IS NOT NULL) AND (workload_issuer_ref_project_id IS NOT NULL) AND (workload_issuer_id IS NOT NULL) AND (workload_issuer_ref_kind IS NOT NULL) AND (workload_issuer_ref_organization_id = organization_id) AND (workload_issuer_ref_project_id = project_id) AND (workload_issuer_id = original_workload_issuer_id) AND (workload_issuer_ref_kind = 'system'::text))),
  CONSTRAINT "trigger_workload_bindings_project_ref_check" CHECK (((project_ref_organization_id IS NULL) AND (project_ref_id IS NULL)) OR ((project_ref_organization_id IS NOT NULL) AND (project_ref_id IS NOT NULL) AND (project_ref_organization_id = organization_id) AND (project_ref_id = project_id))),
  CONSTRAINT "trigger_workload_bindings_trigger_ref_check" CHECK (((trigger_ref_organization_id IS NULL) AND (trigger_ref_project_id IS NULL) AND (trigger_id IS NULL)) OR ((trigger_ref_organization_id IS NOT NULL) AND (trigger_ref_project_id IS NOT NULL) AND (trigger_id IS NOT NULL) AND (trigger_ref_organization_id = organization_id) AND (trigger_ref_project_id = project_id) AND (trigger_id = original_trigger_id)))
);
-- Create index "trigger_workload_bindings_assistant_ref_idx" to table: "trigger_workload_bindings"
CREATE INDEX "trigger_workload_bindings_assistant_ref_idx" ON "trigger_workload_bindings" ("assistant_binding_ref_organization_id", "assistant_binding_ref_project_id", "assistant_binding_id");
-- Create index "trigger_workload_bindings_issuer_ref_idx" to table: "trigger_workload_bindings"
CREATE INDEX "trigger_workload_bindings_issuer_ref_idx" ON "trigger_workload_bindings" ("workload_issuer_ref_organization_id", "workload_issuer_ref_project_id", "workload_issuer_id", "workload_issuer_ref_kind");
-- Create index "trigger_workload_bindings_live_subject_key" to table: "trigger_workload_bindings"
CREATE UNIQUE INDEX "trigger_workload_bindings_live_subject_key" ON "trigger_workload_bindings" ("organization_id", "original_workload_issuer_id", "subject") WHERE (deleted IS FALSE);
-- Create index "trigger_workload_bindings_live_trigger_key" to table: "trigger_workload_bindings"
CREATE UNIQUE INDEX "trigger_workload_bindings_live_trigger_key" ON "trigger_workload_bindings" ("original_trigger_id") WHERE (deleted IS FALSE);
-- Create index "trigger_workload_bindings_project_ref_idx" to table: "trigger_workload_bindings"
CREATE INDEX "trigger_workload_bindings_project_ref_idx" ON "trigger_workload_bindings" ("project_ref_organization_id", "project_ref_id");
-- Create index "trigger_workload_bindings_trigger_generation_key" to table: "trigger_workload_bindings"
CREATE UNIQUE INDEX "trigger_workload_bindings_trigger_generation_key" ON "trigger_workload_bindings" ("original_trigger_id", "generation");
-- Create index "trigger_workload_bindings_trigger_ref_idx" to table: "trigger_workload_bindings"
CREATE INDEX "trigger_workload_bindings_trigger_ref_idx" ON "trigger_workload_bindings" ("trigger_ref_organization_id", "trigger_ref_project_id", "trigger_id");
