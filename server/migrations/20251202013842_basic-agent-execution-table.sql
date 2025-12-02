-- Create "agent_executions" table
CREATE TABLE "agent_executions" (
  "response_id" text NOT NULL,
  "project_id" uuid NOT NULL,
  "status" text NOT NULL,
  "started_at" timestamptz NOT NULL,
  "completed_at" timestamptz NULL,
  "deleted_at" timestamptz NULL,
  "deleted" boolean NOT NULL GENERATED ALWAYS AS (deleted_at IS NOT NULL) STORED,
  PRIMARY KEY ("response_id"),
  CONSTRAINT "agent_executions_project_id_fkey" FOREIGN KEY ("project_id") REFERENCES "projects" ("id") ON UPDATE NO ACTION ON DELETE SET NULL
);
-- Create index "agent_executions_project_id_started_at_idx" to table: "agent_executions"
CREATE INDEX "agent_executions_project_id_started_at_idx" ON "agent_executions" ("project_id", "started_at") WHERE (deleted IS FALSE);
