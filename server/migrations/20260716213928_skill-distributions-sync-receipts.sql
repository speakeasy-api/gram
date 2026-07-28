-- atlas:txmode none

-- Create index "plugins_project_id_id_key" to table: "plugins"
CREATE UNIQUE INDEX CONCURRENTLY "plugins_project_id_id_key" ON "plugins" ("project_id", "id");
-- Create index "skills_project_id_id_key" to table: "skills"
CREATE UNIQUE INDEX CONCURRENTLY "skills_project_id_id_key" ON "skills" ("project_id", "id");
-- Create index "skill_versions_skill_id_id_key" to table: "skill_versions"
CREATE UNIQUE INDEX CONCURRENTLY "skill_versions_skill_id_id_key" ON "skill_versions" ("skill_id", "id");
-- Create "skill_distributions" table
CREATE TABLE "skill_distributions" (
  "id" uuid NOT NULL DEFAULT generate_uuidv7(),
  "project_id" uuid NOT NULL,
  "skill_id" uuid NOT NULL,
  "pinned_version_id" uuid NULL,
  "plugin_id" uuid NULL,
  "channel" text NOT NULL,
  "created_by_user_id" text NOT NULL,
  "revoked_at" timestamptz NULL,
  "created_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "updated_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  PRIMARY KEY ("id"),
  CONSTRAINT "skill_distributions_project_id_fkey" FOREIGN KEY ("project_id") REFERENCES "projects" ("id") ON UPDATE NO ACTION ON DELETE CASCADE,
  CONSTRAINT "skill_distributions_project_id_plugin_id_fkey" FOREIGN KEY ("project_id", "plugin_id") REFERENCES "plugins" ("project_id", "id") ON UPDATE NO ACTION ON DELETE NO ACTION,
  CONSTRAINT "skill_distributions_project_id_skill_id_fkey" FOREIGN KEY ("project_id", "skill_id") REFERENCES "skills" ("project_id", "id") ON UPDATE NO ACTION ON DELETE CASCADE,
  CONSTRAINT "skill_distributions_skill_id_pinned_version_id_fkey" FOREIGN KEY ("skill_id", "pinned_version_id") REFERENCES "skill_versions" ("skill_id", "id") ON UPDATE NO ACTION ON DELETE NO ACTION
);
-- Create index "skill_distributions_plugin_id_idx" to table: "skill_distributions"
CREATE INDEX "skill_distributions_plugin_id_idx" ON "skill_distributions" ("plugin_id");
-- Create index "skill_distributions_project_id_idx" to table: "skill_distributions"
CREATE INDEX "skill_distributions_project_id_idx" ON "skill_distributions" ("project_id");
-- Create index "skill_distributions_project_id_skill_id_channel_plugin_id_key" to table: "skill_distributions"
CREATE UNIQUE INDEX "skill_distributions_project_id_skill_id_channel_plugin_id_key" ON "skill_distributions" ("project_id", "skill_id", "channel", "plugin_id") NULLS NOT DISTINCT WHERE (revoked_at IS NULL);
-- Create index "skill_distributions_skill_id_pinned_version_id_idx" to table: "skill_distributions"
CREATE INDEX "skill_distributions_skill_id_pinned_version_id_idx" ON "skill_distributions" ("skill_id", "pinned_version_id");
-- Create "skill_sync_receipts" table
CREATE TABLE "skill_sync_receipts" (
  "project_id" uuid NOT NULL,
  "skill_id" uuid NOT NULL,
  "skill_version_id" uuid NULL,
  "user_id" text NOT NULL,
  "hostname" text NOT NULL,
  "provider" text NOT NULL,
  "status" text NOT NULL,
  "synced_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "created_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "updated_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  PRIMARY KEY ("project_id", "skill_id", "user_id", "hostname", "provider"),
  CONSTRAINT "skill_sync_receipts_project_id_fkey" FOREIGN KEY ("project_id") REFERENCES "projects" ("id") ON UPDATE NO ACTION ON DELETE CASCADE,
  CONSTRAINT "skill_sync_receipts_project_id_skill_id_fkey" FOREIGN KEY ("project_id", "skill_id") REFERENCES "skills" ("project_id", "id") ON UPDATE NO ACTION ON DELETE CASCADE,
  CONSTRAINT "skill_sync_receipts_skill_id_skill_version_id_fkey" FOREIGN KEY ("skill_id", "skill_version_id") REFERENCES "skill_versions" ("skill_id", "id") ON UPDATE NO ACTION ON DELETE NO ACTION
);
-- Create index "skill_sync_receipts_project_id_skill_version_id_idx" to table: "skill_sync_receipts"
CREATE INDEX "skill_sync_receipts_project_id_skill_version_id_idx" ON "skill_sync_receipts" ("project_id", "skill_version_id");
-- Create index "skill_sync_receipts_project_id_user_id_hostname_provider_idx" to table: "skill_sync_receipts"
CREATE INDEX "skill_sync_receipts_project_id_user_id_hostname_provider_idx" ON "skill_sync_receipts" ("project_id", "user_id", "hostname", "provider", "skill_id");
-- Create index "skill_sync_receipts_skill_id_skill_version_id_idx" to table: "skill_sync_receipts"
CREATE INDEX "skill_sync_receipts_skill_id_skill_version_id_idx" ON "skill_sync_receipts" ("skill_id", "skill_version_id");
