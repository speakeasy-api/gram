-- Create "agent_mcp_project_boundary_shadow_counts" table
CREATE TABLE "agent_mcp_project_boundary_shadow_counts" (
  "organization_id" text NOT NULL,
  "agent_id" uuid NOT NULL,
  "agent_project_id" uuid NULL,
  "server_id" uuid NOT NULL,
  "server_project_id" uuid NOT NULL,
  "request_count" bigint NOT NULL DEFAULT 1,
  "first_observed_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "last_observed_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  PRIMARY KEY ("organization_id", "agent_id", "server_id"),
  CONSTRAINT "agent_mcp_project_boundary_shadow_counts_agent_fkey" FOREIGN KEY ("organization_id", "agent_id") REFERENCES "agents" ("organization_id", "id") ON UPDATE NO ACTION ON DELETE CASCADE,
  CONSTRAINT "agent_mcp_project_boundary_shadow_counts_agent_project_fkey" FOREIGN KEY ("organization_id", "agent_project_id") REFERENCES "projects" ("organization_id", "id") ON UPDATE NO ACTION ON DELETE CASCADE,
  CONSTRAINT "agent_mcp_project_boundary_shadow_counts_server_fkey" FOREIGN KEY ("server_project_id", "server_id") REFERENCES "mcp_servers" ("project_id", "id") ON UPDATE NO ACTION ON DELETE CASCADE,
  CONSTRAINT "agent_mcp_project_boundary_shadow_counts_server_project_fkey" FOREIGN KEY ("organization_id", "server_project_id") REFERENCES "projects" ("organization_id", "id") ON UPDATE NO ACTION ON DELETE CASCADE
);
-- Create index "agent_mcp_project_boundary_shadow_counts_org_agent_idx" to table: "agent_mcp_project_boundary_shadow_counts"
CREATE INDEX "agent_mcp_project_boundary_shadow_counts_org_agent_idx" ON "agent_mcp_project_boundary_shadow_counts" ("organization_id", "agent_id");
-- Set comment to column: "organization_id" on table: "agent_mcp_project_boundary_shadow_counts"
COMMENT ON COLUMN "agent_mcp_project_boundary_shadow_counts"."organization_id" IS '@access: confidential';
-- Set comment to column: "agent_id" on table: "agent_mcp_project_boundary_shadow_counts"
COMMENT ON COLUMN "agent_mcp_project_boundary_shadow_counts"."agent_id" IS '@access: confidential';
-- Set comment to column: "agent_project_id" on table: "agent_mcp_project_boundary_shadow_counts"
COMMENT ON COLUMN "agent_mcp_project_boundary_shadow_counts"."agent_project_id" IS '@access: confidential';
-- Set comment to column: "server_id" on table: "agent_mcp_project_boundary_shadow_counts"
COMMENT ON COLUMN "agent_mcp_project_boundary_shadow_counts"."server_id" IS '@access: confidential';
-- Set comment to column: "server_project_id" on table: "agent_mcp_project_boundary_shadow_counts"
COMMENT ON COLUMN "agent_mcp_project_boundary_shadow_counts"."server_project_id" IS '@access: confidential';
-- Set comment to column: "request_count" on table: "agent_mcp_project_boundary_shadow_counts"
COMMENT ON COLUMN "agent_mcp_project_boundary_shadow_counts"."request_count" IS '@access: confidential';
-- Set comment to column: "first_observed_at" on table: "agent_mcp_project_boundary_shadow_counts"
COMMENT ON COLUMN "agent_mcp_project_boundary_shadow_counts"."first_observed_at" IS '@access: confidential';
-- Set comment to column: "last_observed_at" on table: "agent_mcp_project_boundary_shadow_counts"
COMMENT ON COLUMN "agent_mcp_project_boundary_shadow_counts"."last_observed_at" IS '@access: confidential';
