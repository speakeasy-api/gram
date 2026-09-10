-- Create "agent_role_assignments" table
CREATE TABLE "agent_role_assignments" (
  "id" uuid NOT NULL DEFAULT generate_uuidv7(),
  "organization_id" text NOT NULL,
  "agent_id" uuid NOT NULL,
  "role_urn" text NOT NULL,
  "created_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "updated_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "deleted_at" timestamptz NULL,
  PRIMARY KEY ("id"),
  CONSTRAINT "agent_role_assignments_agent_tenant_fkey" FOREIGN KEY ("organization_id", "agent_id") REFERENCES "agents" ("organization_id", "id") ON UPDATE NO ACTION ON DELETE CASCADE,
  CONSTRAINT "agent_role_assignments_organization_id_fkey" FOREIGN KEY ("organization_id") REFERENCES "organization_metadata" ("id") ON UPDATE NO ACTION ON DELETE CASCADE
);
-- Create index "agent_role_assignments_org_agent_all_idx" to table: "agent_role_assignments"
CREATE INDEX "agent_role_assignments_org_agent_all_idx" ON "agent_role_assignments" ("organization_id", "agent_id");
-- Create index "agent_role_assignments_org_agent_role_key" to table: "agent_role_assignments"
CREATE UNIQUE INDEX "agent_role_assignments_org_agent_role_key" ON "agent_role_assignments" ("organization_id", "agent_id", "role_urn") WHERE (deleted_at IS NULL);
-- Create index "agent_role_assignments_org_role_idx" to table: "agent_role_assignments"
CREATE INDEX "agent_role_assignments_org_role_idx" ON "agent_role_assignments" ("organization_id", "role_urn") WHERE (deleted_at IS NULL);
