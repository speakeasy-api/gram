-- Create "plugin_repository_bindings" table
CREATE TABLE "plugin_repository_bindings" (
  "project_id" uuid NOT NULL,
  "repository_id" bigint NOT NULL,
  "repo_owner" text NOT NULL,
  "repo_name" text NOT NULL,
  "installation_id" bigint NOT NULL,
  "created_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "updated_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  PRIMARY KEY ("project_id"),
  CONSTRAINT "plugin_repository_bindings_project_id_fkey" FOREIGN KEY ("project_id") REFERENCES "projects" ("id") ON UPDATE NO ACTION ON DELETE SET NULL,
  CONSTRAINT "plugin_repository_bindings_repo_name_check" CHECK (repo_name <> ''::text),
  CONSTRAINT "plugin_repository_bindings_repo_owner_check" CHECK (repo_owner <> ''::text)
);
-- Create index "plugin_repository_bindings_repository_id_key" to table: "plugin_repository_bindings"
CREATE UNIQUE INDEX "plugin_repository_bindings_repository_id_key" ON "plugin_repository_bindings" ("repository_id");
-- Set comment to column: "project_id" on table: "plugin_repository_bindings"
COMMENT ON COLUMN "plugin_repository_bindings"."project_id" IS '@access: confidential';
-- Set comment to column: "repository_id" on table: "plugin_repository_bindings"
COMMENT ON COLUMN "plugin_repository_bindings"."repository_id" IS '@access: restricted';
-- Set comment to column: "repo_owner" on table: "plugin_repository_bindings"
COMMENT ON COLUMN "plugin_repository_bindings"."repo_owner" IS '@access: confidential-pii';
-- Set comment to column: "repo_name" on table: "plugin_repository_bindings"
COMMENT ON COLUMN "plugin_repository_bindings"."repo_name" IS '@access: opaque-confidential';
-- Set comment to column: "installation_id" on table: "plugin_repository_bindings"
COMMENT ON COLUMN "plugin_repository_bindings"."installation_id" IS '@access: restricted';
-- Set comment to column: "created_at" on table: "plugin_repository_bindings"
COMMENT ON COLUMN "plugin_repository_bindings"."created_at" IS '@access: confidential';
-- Set comment to column: "updated_at" on table: "plugin_repository_bindings"
COMMENT ON COLUMN "plugin_repository_bindings"."updated_at" IS '@access: confidential';
