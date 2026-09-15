-- atlas:txmode none

-- Drop index "platform_mcp_onboarding_milestones_connection_generation_key" from table: "platform_mcp_onboarding_milestones"
DROP INDEX CONCURRENTLY "platform_mcp_onboarding_milestones_connection_generation_key";
-- Modify "platform_mcp_onboarding_milestones" table
ALTER TABLE "platform_mcp_onboarding_milestones" DROP CONSTRAINT "platform_mcp_onboarding_milestones_connection_generation_check", ADD CONSTRAINT "platform_mcp_onboarding_milestones_connection_generation_check" CHECK ((milestone <> ALL (ARRAY['authorization_succeeded'::text, 'authorization_failed'::text, 'connection_ready'::text, 'catalog_explored'::text, 'first_read_succeeded'::text, 'first_write_succeeded'::text, 'read_only_cohort'::text])) OR ((connection_id IS NOT NULL) AND (connection_generation IS NOT NULL))) NOT VALID;
ALTER TABLE "platform_mcp_onboarding_milestones" VALIDATE CONSTRAINT "platform_mcp_onboarding_milestones_connection_generation_check";
-- Create index "platform_mcp_onboarding_milestones_connection_generation_key" to table: "platform_mcp_onboarding_milestones"
CREATE UNIQUE INDEX CONCURRENTLY "platform_mcp_onboarding_milestones_connection_generation_key" ON "platform_mcp_onboarding_milestones" ("milestone", "connection_id", "connection_generation") WHERE ((connection_id IS NOT NULL) AND (connection_generation IS NOT NULL) AND (milestone = ANY (ARRAY['authorization_succeeded'::text, 'authorization_failed'::text, 'connection_ready'::text, 'catalog_explored'::text, 'first_read_succeeded'::text, 'first_write_succeeded'::text, 'read_only_cohort'::text])));
-- Create index "plugin_servers_plugin_id_id_key" to table: "plugin_servers"
CREATE UNIQUE INDEX CONCURRENTLY "plugin_servers_plugin_id_id_key" ON "plugin_servers" ("plugin_id", "id");
-- Create "platform_mcp_distributions" table
CREATE TABLE "platform_mcp_distributions" (
  "id" uuid NOT NULL DEFAULT generate_uuidv7(),
  "organization_id" text NOT NULL,
  "project_id" uuid NOT NULL,
  "registration_id" uuid NOT NULL,
  "default_plugin_id" uuid NOT NULL,
  "plugin_server_id" uuid NULL,
  "state" text NOT NULL,
  "version" bigint NOT NULL,
  "attachment_was_created" boolean NOT NULL,
  "publication_state" text NOT NULL DEFAULT 'pending',
  "publication_updated_at" timestamptz NULL,
  "connection_id" uuid NOT NULL,
  "connection_generation" uuid NOT NULL,
  "created_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "updated_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  PRIMARY KEY ("id"),
  CONSTRAINT "platform_mcp_distributions_default_plugin_plugin_server_fkey" FOREIGN KEY ("default_plugin_id", "plugin_server_id") REFERENCES "plugin_servers" ("plugin_id", "id") ON UPDATE NO ACTION ON DELETE NO ACTION,
  CONSTRAINT "platform_mcp_distributions_organization_connection_fkey" FOREIGN KEY ("organization_id", "connection_id") REFERENCES "platform_mcp_connections" ("organization_id", "id") ON UPDATE NO ACTION ON DELETE CASCADE,
  CONSTRAINT "platform_mcp_distributions_organization_id_fkey" FOREIGN KEY ("organization_id") REFERENCES "organization_metadata" ("id") ON UPDATE NO ACTION ON DELETE CASCADE,
  CONSTRAINT "platform_mcp_distributions_organization_project_fkey" FOREIGN KEY ("organization_id", "project_id") REFERENCES "projects" ("organization_id", "id") ON UPDATE NO ACTION ON DELETE CASCADE,
  CONSTRAINT "platform_mcp_distributions_project_default_plugin_fkey" FOREIGN KEY ("project_id", "default_plugin_id") REFERENCES "plugins" ("project_id", "id") ON UPDATE NO ACTION ON DELETE CASCADE,
  CONSTRAINT "platform_mcp_distributions_project_registration_fkey" FOREIGN KEY ("project_id", "registration_id") REFERENCES "platform_mcp_catalog_registrations" ("project_id", "id") ON UPDATE NO ACTION ON DELETE CASCADE,
  CONSTRAINT "platform_mcp_distributions_state_check" CHECK (state <> ''::text),
  CONSTRAINT "platform_mcp_distributions_version_check" CHECK (version > 0)
);
-- Create index "platform_mcp_distributions_identity_key" to table: "platform_mcp_distributions"
CREATE UNIQUE INDEX "platform_mcp_distributions_identity_key" ON "platform_mcp_distributions" ("organization_id", "project_id", "registration_id", "default_plugin_id");
-- Create index "platform_mcp_distributions_organization_connection_idx" to table: "platform_mcp_distributions"
CREATE INDEX "platform_mcp_distributions_organization_connection_idx" ON "platform_mcp_distributions" ("organization_id", "connection_id");
-- Create index "platform_mcp_distributions_project_default_plugin_idx" to table: "platform_mcp_distributions"
CREATE INDEX "platform_mcp_distributions_project_default_plugin_idx" ON "platform_mcp_distributions" ("project_id", "default_plugin_id");
-- Create index "platform_mcp_distributions_project_plugin_server_idx" to table: "platform_mcp_distributions"
CREATE INDEX "platform_mcp_distributions_project_plugin_server_idx" ON "platform_mcp_distributions" ("project_id", "plugin_server_id") WHERE (plugin_server_id IS NOT NULL);
-- Create index "platform_mcp_distributions_project_registration_id_key" to table: "platform_mcp_distributions"
CREATE UNIQUE INDEX "platform_mcp_distributions_project_registration_id_key" ON "platform_mcp_distributions" ("project_id", "registration_id", "id");
-- Create "platform_mcp_onboarding_workflows" table
CREATE TABLE "platform_mcp_onboarding_workflows" (
  "id" uuid NOT NULL DEFAULT generate_uuidv7(),
  "organization_id" text NOT NULL,
  "initiating_subject_urn" text NOT NULL,
  "source_surface" text NOT NULL,
  "client_family" text NOT NULL,
  "agent_configuration_copied_at" timestamptz NULL,
  "connection_id" uuid NULL,
  "connection_generation" uuid NULL,
  "selected_project_id" uuid NULL,
  "selected_registration_id" uuid NULL,
  "status" text NOT NULL DEFAULT 'active',
  "correlation_id" uuid NOT NULL DEFAULT generate_uuidv7(),
  "expires_at" timestamptz NOT NULL,
  "closed_at" timestamptz NULL,
  "created_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "updated_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  PRIMARY KEY ("id"),
  CONSTRAINT "platform_mcp_onboarding_workflows_organization_connection_fkey" FOREIGN KEY ("organization_id", "connection_id") REFERENCES "platform_mcp_connections" ("organization_id", "id") ON UPDATE NO ACTION ON DELETE NO ACTION,
  CONSTRAINT "platform_mcp_onboarding_workflows_organization_id_fkey" FOREIGN KEY ("organization_id") REFERENCES "organization_metadata" ("id") ON UPDATE NO ACTION ON DELETE CASCADE,
  CONSTRAINT "platform_mcp_onboarding_workflows_organization_project_fkey" FOREIGN KEY ("organization_id", "selected_project_id") REFERENCES "projects" ("organization_id", "id") ON UPDATE NO ACTION ON DELETE NO ACTION,
  CONSTRAINT "platform_mcp_onboarding_workflows_project_registration_fkey" FOREIGN KEY ("selected_project_id", "selected_registration_id") REFERENCES "platform_mcp_catalog_registrations" ("project_id", "id") ON UPDATE NO ACTION ON DELETE NO ACTION,
  CONSTRAINT "platform_mcp_onboarding_workflows_client_family_check" CHECK (client_family <> ''::text),
  CONSTRAINT "platform_mcp_onboarding_workflows_connection_generation_check" CHECK ((connection_id IS NULL) = (connection_generation IS NULL)),
  CONSTRAINT "platform_mcp_onboarding_workflows_initiating_subject_urn_check" CHECK (initiating_subject_urn <> ''::text),
  CONSTRAINT "platform_mcp_onboarding_workflows_selected_target_check" CHECK ((selected_project_id IS NULL) = (selected_registration_id IS NULL)),
  CONSTRAINT "platform_mcp_onboarding_workflows_source_surface_check" CHECK (source_surface <> ''::text),
  CONSTRAINT "platform_mcp_onboarding_workflows_status_check" CHECK (status <> ''::text)
);
-- Create index "platform_mcp_onboarding_workflows_active_subject_key" to table: "platform_mcp_onboarding_workflows"
CREATE UNIQUE INDEX "platform_mcp_onboarding_workflows_active_subject_key" ON "platform_mcp_onboarding_workflows" ("organization_id", "initiating_subject_urn") WHERE (status = 'active'::text);
-- Create index "platform_mcp_onboarding_workflows_correlation_id_key" to table: "platform_mcp_onboarding_workflows"
CREATE UNIQUE INDEX "platform_mcp_onboarding_workflows_correlation_id_key" ON "platform_mcp_onboarding_workflows" ("correlation_id");
-- Create index "platform_mcp_onboarding_workflows_expires_at_idx" to table: "platform_mcp_onboarding_workflows"
CREATE INDEX "platform_mcp_onboarding_workflows_expires_at_idx" ON "platform_mcp_onboarding_workflows" ("expires_at") WHERE (status = 'active'::text);
-- Create index "platform_mcp_onboarding_workflows_organization_id_id_key" to table: "platform_mcp_onboarding_workflows"
CREATE UNIQUE INDEX "platform_mcp_onboarding_workflows_organization_id_id_key" ON "platform_mcp_onboarding_workflows" ("organization_id", "id");
-- Create index "platform_mcp_onboarding_workflows_organization_updated_at_idx" to table: "platform_mcp_onboarding_workflows"
CREATE INDEX "platform_mcp_onboarding_workflows_organization_updated_at_idx" ON "platform_mcp_onboarding_workflows" ("organization_id", "updated_at" DESC);
-- Create "platform_mcp_feedback" table
CREATE TABLE "platform_mcp_feedback" (
  "id" uuid NOT NULL DEFAULT generate_uuidv7(),
  "organization_id" text NOT NULL,
  "subject_urn" text NOT NULL,
  "connection_id" uuid NULL,
  "connection_generation" uuid NULL,
  "project_id" uuid NULL,
  "workflow_id" uuid NULL,
  "request_reference" text NULL,
  "category" text NOT NULL,
  "idempotency_key" text NOT NULL,
  "input_hash" text NOT NULL,
  "rating" integer NULL,
  "success" boolean NULL,
  "tool_name" text NULL,
  "failure_category" text NULL,
  "note" text NULL,
  "delivery_state" text NOT NULL DEFAULT 'queued',
  "delivery_attempts" integer NOT NULL DEFAULT 0,
  "last_delivery_attempt_at" timestamptz NULL,
  "delivered_at" timestamptz NULL,
  "dead_lettered_at" timestamptz NULL,
  "expires_at" timestamptz NOT NULL,
  "created_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "updated_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  PRIMARY KEY ("id"),
  CONSTRAINT "platform_mcp_feedback_organization_connection_fkey" FOREIGN KEY ("organization_id", "connection_id") REFERENCES "platform_mcp_connections" ("organization_id", "id") ON UPDATE NO ACTION ON DELETE NO ACTION,
  CONSTRAINT "platform_mcp_feedback_organization_id_fkey" FOREIGN KEY ("organization_id") REFERENCES "organization_metadata" ("id") ON UPDATE NO ACTION ON DELETE CASCADE,
  CONSTRAINT "platform_mcp_feedback_organization_project_fkey" FOREIGN KEY ("organization_id", "project_id") REFERENCES "projects" ("organization_id", "id") ON UPDATE NO ACTION ON DELETE NO ACTION,
  CONSTRAINT "platform_mcp_feedback_organization_workflow_fkey" FOREIGN KEY ("organization_id", "workflow_id") REFERENCES "platform_mcp_onboarding_workflows" ("organization_id", "id") ON UPDATE NO ACTION ON DELETE NO ACTION,
  CONSTRAINT "platform_mcp_feedback_category_check" CHECK (category <> ''::text),
  CONSTRAINT "platform_mcp_feedback_connection_generation_check" CHECK ((connection_id IS NULL) = (connection_generation IS NULL)),
  CONSTRAINT "platform_mcp_feedback_delivery_attempts_check" CHECK (delivery_attempts >= 0),
  CONSTRAINT "platform_mcp_feedback_delivery_state_check" CHECK (delivery_state <> ''::text),
  CONSTRAINT "platform_mcp_feedback_idempotency_key_check" CHECK ((idempotency_key <> ''::text) AND (char_length(idempotency_key) <= 128)),
  CONSTRAINT "platform_mcp_feedback_input_hash_check" CHECK (input_hash <> ''::text),
  CONSTRAINT "platform_mcp_feedback_note_check" CHECK ((note IS NULL) OR (char_length(note) <= 500)),
  CONSTRAINT "platform_mcp_feedback_rating_check" CHECK ((rating IS NULL) OR ((rating >= 1) AND (rating <= 5))),
  CONSTRAINT "platform_mcp_feedback_subject_urn_check" CHECK (subject_urn <> ''::text)
);
-- Create index "platform_mcp_feedback_expires_at_idx" to table: "platform_mcp_feedback"
CREATE INDEX "platform_mcp_feedback_expires_at_idx" ON "platform_mcp_feedback" ("expires_at");
-- Create index "platform_mcp_feedback_organization_connection_created_at_idx" to table: "platform_mcp_feedback"
CREATE INDEX "platform_mcp_feedback_organization_connection_created_at_idx" ON "platform_mcp_feedback" ("organization_id", "connection_id", "created_at" DESC) WHERE (connection_id IS NOT NULL);
-- Create index "platform_mcp_feedback_organization_created_at_idx" to table: "platform_mcp_feedback"
CREATE INDEX "platform_mcp_feedback_organization_created_at_idx" ON "platform_mcp_feedback" ("organization_id", "created_at" DESC);
-- Create index "platform_mcp_feedback_organization_subject_created_at_idx" to table: "platform_mcp_feedback"
CREATE INDEX "platform_mcp_feedback_organization_subject_created_at_idx" ON "platform_mcp_feedback" ("organization_id", "subject_urn", "created_at" DESC);
-- Create index "platform_mcp_feedback_organization_subject_idempotency_key_idx" to table: "platform_mcp_feedback"
CREATE UNIQUE INDEX "platform_mcp_feedback_organization_subject_idempotency_key_idx" ON "platform_mcp_feedback" ("organization_id", "subject_urn", "idempotency_key");
-- Create index "platform_mcp_feedback_organization_workflow_created_at_idx" to table: "platform_mcp_feedback"
CREATE INDEX "platform_mcp_feedback_organization_workflow_created_at_idx" ON "platform_mcp_feedback" ("organization_id", "workflow_id", "created_at" DESC) WHERE (workflow_id IS NOT NULL);
-- Create "platform_mcp_selected_use_evidence" table
CREATE TABLE "platform_mcp_selected_use_evidence" (
  "id" uuid NOT NULL DEFAULT generate_uuidv7(),
  "organization_id" text NOT NULL,
  "project_id" uuid NOT NULL,
  "registration_id" uuid NOT NULL,
  "distribution_id" uuid NOT NULL,
  "distribution_version" bigint NOT NULL,
  "workflow_id" uuid NULL,
  "tool_name" text NOT NULL,
  "tool_category" text NOT NULL,
  "request_reference" text NULL,
  "succeeded_at" timestamptz NOT NULL,
  "created_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  PRIMARY KEY ("id"),
  CONSTRAINT "platform_mcp_selected_use_evidence_organization_id_fkey" FOREIGN KEY ("organization_id") REFERENCES "organization_metadata" ("id") ON UPDATE NO ACTION ON DELETE CASCADE,
  CONSTRAINT "platform_mcp_selected_use_evidence_organization_project_fkey" FOREIGN KEY ("organization_id", "project_id") REFERENCES "projects" ("organization_id", "id") ON UPDATE NO ACTION ON DELETE NO ACTION,
  CONSTRAINT "platform_mcp_selected_use_evidence_organization_workflow_fkey" FOREIGN KEY ("organization_id", "workflow_id") REFERENCES "platform_mcp_onboarding_workflows" ("organization_id", "id") ON UPDATE NO ACTION ON DELETE NO ACTION,
  CONSTRAINT "platform_mcp_selected_use_evidence_project_registration_distrib" FOREIGN KEY ("project_id", "registration_id", "distribution_id") REFERENCES "platform_mcp_distributions" ("project_id", "registration_id", "id") ON UPDATE NO ACTION ON DELETE NO ACTION,
  CONSTRAINT "platform_mcp_selected_use_evidence_project_registration_fkey" FOREIGN KEY ("project_id", "registration_id") REFERENCES "platform_mcp_catalog_registrations" ("project_id", "id") ON UPDATE NO ACTION ON DELETE NO ACTION,
  CONSTRAINT "platform_mcp_selected_use_evidence_distribution_version_check" CHECK (distribution_version > 0),
  CONSTRAINT "platform_mcp_selected_use_evidence_tool_category_check" CHECK (tool_category <> ''::text),
  CONSTRAINT "platform_mcp_selected_use_evidence_tool_name_check" CHECK (tool_name <> ''::text)
);
-- Create index "platform_mcp_selected_use_evidence_distribution_version_key" to table: "platform_mcp_selected_use_evidence"
CREATE UNIQUE INDEX "platform_mcp_selected_use_evidence_distribution_version_key" ON "platform_mcp_selected_use_evidence" ("distribution_id", "distribution_version");
-- Create index "platform_mcp_selected_use_evidence_organization_project_succeed" to table: "platform_mcp_selected_use_evidence"
CREATE INDEX "platform_mcp_selected_use_evidence_organization_project_succeed" ON "platform_mcp_selected_use_evidence" ("organization_id", "project_id", "succeeded_at" DESC);
-- Create index "platform_mcp_selected_use_evidence_project_registration_distrib" to table: "platform_mcp_selected_use_evidence"
CREATE INDEX "platform_mcp_selected_use_evidence_project_registration_distrib" ON "platform_mcp_selected_use_evidence" ("project_id", "registration_id", "distribution_id");
-- Create index "platform_mcp_selected_use_evidence_project_registration_idx" to table: "platform_mcp_selected_use_evidence"
CREATE INDEX "platform_mcp_selected_use_evidence_project_registration_idx" ON "platform_mcp_selected_use_evidence" ("project_id", "registration_id");
-- Create index "platform_mcp_selected_use_evidence_workflow_id_idx" to table: "platform_mcp_selected_use_evidence"
CREATE INDEX "platform_mcp_selected_use_evidence_workflow_id_idx" ON "platform_mcp_selected_use_evidence" ("workflow_id") WHERE (workflow_id IS NOT NULL);
