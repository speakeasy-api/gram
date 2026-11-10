-- Create "okta_server_suggestion_dismissals" table
CREATE TABLE "okta_server_suggestion_dismissals" (
  "organization_id" text NOT NULL,
  "registry_entry_id" uuid NOT NULL,
  "created_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "updated_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  PRIMARY KEY ("organization_id", "registry_entry_id"),
  CONSTRAINT "okta_server_suggestion_dismissals_organization_id_fkey" FOREIGN KEY ("organization_id") REFERENCES "organization_metadata" ("id") ON UPDATE NO ACTION ON DELETE CASCADE,
  CONSTRAINT "okta_server_suggestion_dismissals_registry_entry_id_fkey" FOREIGN KEY ("registry_entry_id") REFERENCES "mcp_registry_entries" ("id") ON UPDATE NO ACTION ON DELETE CASCADE
);
-- Create index "okta_server_suggestion_dismissals_registry_entry_id_idx" to table: "okta_server_suggestion_dismissals"
CREATE INDEX "okta_server_suggestion_dismissals_registry_entry_id_idx" ON "okta_server_suggestion_dismissals" ("registry_entry_id");
