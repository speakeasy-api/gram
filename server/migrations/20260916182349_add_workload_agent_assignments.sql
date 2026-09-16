-- Create "workload_agent_assignments" table
CREATE TABLE "workload_agent_assignments" (
  "id" uuid NOT NULL DEFAULT generate_uuidv7(),
  "organization_id" text NOT NULL,
  "workload_issuer_id" uuid NOT NULL,
  "subject" text NOT NULL,
  "agent_id" uuid NOT NULL,
  "created_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "updated_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "deleted_at" timestamptz NULL,
  "deleted" boolean NOT NULL GENERATED ALWAYS AS (deleted_at IS NOT NULL) STORED,
  PRIMARY KEY ("id"),
  CONSTRAINT "workload_agent_assignments_agent_fkey" FOREIGN KEY ("organization_id", "agent_id") REFERENCES "agents" ("organization_id", "id") ON UPDATE NO ACTION ON DELETE CASCADE,
  CONSTRAINT "workload_agent_assignments_organization_id_fkey" FOREIGN KEY ("organization_id") REFERENCES "organization_metadata" ("id") ON UPDATE NO ACTION ON DELETE CASCADE,
  CONSTRAINT "workload_agent_assignments_workload_issuer_fkey" FOREIGN KEY ("organization_id", "workload_issuer_id") REFERENCES "workload_issuers" ("organization_id", "id") ON UPDATE NO ACTION ON DELETE CASCADE,
  CONSTRAINT "workload_agent_assignments_subject_check" CHECK (subject <> ''::text)
);
-- Create index "workload_agent_assignments_agent_idx" to table: "workload_agent_assignments"
CREATE INDEX "workload_agent_assignments_agent_idx" ON "workload_agent_assignments" ("organization_id", "agent_id");
-- Create index "workload_agent_assignments_workload_issuer_idx" to table: "workload_agent_assignments"
CREATE INDEX "workload_agent_assignments_workload_issuer_idx" ON "workload_agent_assignments" ("organization_id", "workload_issuer_id");
-- Create index "workload_agent_assignments_workload_key" to table: "workload_agent_assignments"
CREATE UNIQUE INDEX "workload_agent_assignments_workload_key" ON "workload_agent_assignments" ("organization_id", "workload_issuer_id", "subject") WHERE (deleted IS FALSE);
