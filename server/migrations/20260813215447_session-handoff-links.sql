-- Create "session_handoff_links" table
CREATE TABLE "session_handoff_links" (
  "id" uuid NOT NULL DEFAULT generate_uuidv7(),
  "project_id" uuid NOT NULL,
  "organization_id" text NOT NULL,
  "session_id" text NOT NULL,
  "token" text NOT NULL,
  "content" text NOT NULL,
  "created_by_email" text NOT NULL,
  "expires_at" timestamptz NOT NULL,
  "consumed_at" timestamptz NULL,
  "created_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "updated_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  PRIMARY KEY ("id"),
  CONSTRAINT "session_handoff_links_organization_id_fkey" FOREIGN KEY ("organization_id") REFERENCES "organization_metadata" ("id") ON UPDATE NO ACTION ON DELETE CASCADE,
  CONSTRAINT "session_handoff_links_organization_project_fkey" FOREIGN KEY ("organization_id", "project_id") REFERENCES "projects" ("organization_id", "id") ON UPDATE NO ACTION ON DELETE CASCADE
);
-- Create index "session_handoff_links_expires_at_idx" to table: "session_handoff_links"
CREATE INDEX "session_handoff_links_expires_at_idx" ON "session_handoff_links" ("expires_at");
-- Create index "session_handoff_links_organization_project_idx" to table: "session_handoff_links"
CREATE INDEX "session_handoff_links_organization_project_idx" ON "session_handoff_links" ("organization_id", "project_id");
-- Create index "session_handoff_links_token_key" to table: "session_handoff_links"
CREATE UNIQUE INDEX "session_handoff_links_token_key" ON "session_handoff_links" ("token");
