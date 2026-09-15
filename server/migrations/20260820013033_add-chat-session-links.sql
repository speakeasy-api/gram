-- Create "chat_session_links" table
CREATE TABLE "chat_session_links" (
  "id" uuid NOT NULL DEFAULT generate_uuidv7(),
  "project_id" uuid NOT NULL,
  "organization_id" text NOT NULL,
  "parent_chat_id" uuid NOT NULL,
  "child_chat_id" uuid NULL,
  "parent_session_id" text NOT NULL,
  "child_session_id" text NULL,
  "kind" text NOT NULL DEFAULT 'move',
  "target_harness" text NOT NULL,
  "source_surface" text NULL,
  "actor_email" text NULL,
  "device_serial" text NULL,
  "device_hostname" text NULL,
  "created_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "updated_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  PRIMARY KEY ("id"),
  CONSTRAINT "chat_session_links_organization_id_fkey" FOREIGN KEY ("organization_id") REFERENCES "organization_metadata" ("id") ON UPDATE NO ACTION ON DELETE CASCADE,
  CONSTRAINT "chat_session_links_organization_project_fkey" FOREIGN KEY ("organization_id", "project_id") REFERENCES "projects" ("organization_id", "id") ON UPDATE NO ACTION ON DELETE CASCADE
);
-- Create index "chat_session_links_organization_project_idx" to table: "chat_session_links"
CREATE INDEX "chat_session_links_organization_project_idx" ON "chat_session_links" ("organization_id", "project_id");
-- Create index "chat_session_links_project_child_idx" to table: "chat_session_links"
CREATE INDEX "chat_session_links_project_child_idx" ON "chat_session_links" ("project_id", "child_chat_id") WHERE (child_chat_id IS NOT NULL);
-- Create index "chat_session_links_project_parent_child_key" to table: "chat_session_links"
CREATE UNIQUE INDEX "chat_session_links_project_parent_child_key" ON "chat_session_links" ("project_id", "parent_chat_id", "child_chat_id") WHERE (child_chat_id IS NOT NULL);
-- Create index "chat_session_links_project_parent_idx" to table: "chat_session_links"
CREATE INDEX "chat_session_links_project_parent_idx" ON "chat_session_links" ("project_id", "parent_chat_id");
