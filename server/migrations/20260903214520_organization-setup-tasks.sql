-- Create "organization_setup_tasks" table
CREATE TABLE "organization_setup_tasks" (
  "organization_id" text NOT NULL,
  "task_key" text NOT NULL,
  "status" text NOT NULL DEFAULT 'todo',
  "assignee_user_id" text NULL,
  "assignee_email" text NULL,
  "hidden_at" timestamptz NULL,
  "created_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "updated_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  PRIMARY KEY ("organization_id", "task_key"),
  CONSTRAINT "organization_setup_tasks_assignee_user_id_fkey" FOREIGN KEY ("assignee_user_id") REFERENCES "users" ("id") ON UPDATE NO ACTION ON DELETE SET NULL,
  CONSTRAINT "organization_setup_tasks_organization_id_fkey" FOREIGN KEY ("organization_id") REFERENCES "organization_metadata" ("id") ON UPDATE NO ACTION ON DELETE CASCADE,
  CONSTRAINT "organization_setup_tasks_assignee_check" CHECK ((assignee_user_id IS NULL) OR (assignee_email IS NULL))
);
-- Create index "organization_setup_tasks_assignee_user_id_idx" to table: "organization_setup_tasks"
CREATE INDEX "organization_setup_tasks_assignee_user_id_idx" ON "organization_setup_tasks" ("assignee_user_id") WHERE (assignee_user_id IS NOT NULL);
