-- Create "plugin_github_connections" table
CREATE TABLE "plugin_github_connections" (
  "id" uuid NOT NULL DEFAULT generate_uuidv7(),
  "project_id" uuid NOT NULL,
  "installation_id" bigint NOT NULL,
  "repo_owner" text NOT NULL CHECK (repo_owner <> ''),
  "repo_name" text NOT NULL CHECK (repo_name <> ''),
  "created_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "updated_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  PRIMARY KEY ("id"),
  CONSTRAINT "plugin_github_connections_project_id_fkey" FOREIGN KEY ("project_id") REFERENCES "projects" ("id") ON UPDATE NO ACTION ON DELETE CASCADE
);
-- Create index "plugin_github_connections_project_id_key" to table: "plugin_github_connections"
CREATE UNIQUE INDEX "plugin_github_connections_project_id_key" ON "plugin_github_connections" ("project_id");
-- Prevent two projects from writing to the same GitHub repo under the same
-- App installation. GitHub repo owners and names are case-insensitive, so
-- the comparison must be too.
CREATE UNIQUE INDEX "plugin_github_connections_installation_repo_key" ON "plugin_github_connections" ("installation_id", LOWER("repo_owner"), LOWER("repo_name"));
