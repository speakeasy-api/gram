-- Create "project_managed_assistants" table
CREATE TABLE "project_managed_assistants" (
  "project_id" uuid NOT NULL,
  "assistant_id" uuid NOT NULL,
  "created_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "updated_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  PRIMARY KEY ("project_id"),
  CONSTRAINT "project_managed_assistants_assistant_id_fkey" FOREIGN KEY ("assistant_id") REFERENCES "assistants" ("id") ON UPDATE NO ACTION ON DELETE CASCADE,
  CONSTRAINT "project_managed_assistants_project_id_fkey" FOREIGN KEY ("project_id") REFERENCES "projects" ("id") ON UPDATE NO ACTION ON DELETE CASCADE
);
-- Create index "project_managed_assistants_assistant_id_idx" to table: "project_managed_assistants"
CREATE INDEX "project_managed_assistants_assistant_id_idx" ON "project_managed_assistants" ("assistant_id");
