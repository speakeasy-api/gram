-- Create "skill_distributions" table
CREATE TABLE "skill_distributions" (
  "id" uuid NOT NULL DEFAULT generate_uuidv7(),
  "project_id" uuid NOT NULL,
  "skill_id" uuid NOT NULL,
  "pinned_version_id" uuid NULL,
  "audience" text[] NULL,
  "channel" text NOT NULL DEFAULT 'plugin',
  "created_by_user_id" text NOT NULL,
  "revoked_at" timestamptz NULL,
  "created_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "updated_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  PRIMARY KEY ("id"),
  CONSTRAINT "skill_distributions_pinned_version_id_fkey" FOREIGN KEY ("pinned_version_id") REFERENCES "skill_versions" ("id") ON UPDATE NO ACTION ON DELETE SET NULL,
  CONSTRAINT "skill_distributions_project_id_fkey" FOREIGN KEY ("project_id") REFERENCES "projects" ("id") ON UPDATE NO ACTION ON DELETE CASCADE,
  CONSTRAINT "skill_distributions_skill_id_fkey" FOREIGN KEY ("skill_id") REFERENCES "skills" ("id") ON UPDATE NO ACTION ON DELETE CASCADE
);
-- Create index "skill_distributions_pinned_version_id_idx" to table: "skill_distributions"
CREATE INDEX "skill_distributions_pinned_version_id_idx" ON "skill_distributions" ("pinned_version_id");
-- Create index "skill_distributions_project_id_idx" to table: "skill_distributions"
CREATE INDEX "skill_distributions_project_id_idx" ON "skill_distributions" ("project_id");
-- Create index "skill_distributions_project_id_skill_id_channel_key" to table: "skill_distributions"
CREATE UNIQUE INDEX "skill_distributions_project_id_skill_id_channel_key" ON "skill_distributions" ("project_id", "skill_id", "channel") WHERE (revoked_at IS NULL);
-- Create index "skill_distributions_skill_id_idx" to table: "skill_distributions"
CREATE INDEX "skill_distributions_skill_id_idx" ON "skill_distributions" ("skill_id");
-- Create "skill_sync_receipts" table
CREATE TABLE "skill_sync_receipts" (
  "id" uuid NOT NULL DEFAULT generate_uuidv7(),
  "project_id" uuid NOT NULL,
  "skill_id" uuid NOT NULL,
  "skill_version_id" uuid NULL,
  "user_id" text NOT NULL,
  "hostname" text NOT NULL,
  "provider" text NOT NULL DEFAULT 'claude',
  "status" text NOT NULL,
  "synced_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "created_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "updated_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  PRIMARY KEY ("id"),
  CONSTRAINT "skill_sync_receipts_project_id_fkey" FOREIGN KEY ("project_id") REFERENCES "projects" ("id") ON UPDATE NO ACTION ON DELETE CASCADE,
  CONSTRAINT "skill_sync_receipts_skill_id_fkey" FOREIGN KEY ("skill_id") REFERENCES "skills" ("id") ON UPDATE NO ACTION ON DELETE CASCADE,
  CONSTRAINT "skill_sync_receipts_skill_version_id_fkey" FOREIGN KEY ("skill_version_id") REFERENCES "skill_versions" ("id") ON UPDATE NO ACTION ON DELETE SET NULL
);
-- Create index "skill_sync_receipts_project_id_skill_version_id_idx" to table: "skill_sync_receipts"
CREATE INDEX "skill_sync_receipts_project_id_skill_version_id_idx" ON "skill_sync_receipts" ("project_id", "skill_version_id");
-- Create index "skill_sync_receipts_project_skill_user_host_provider_key" to table: "skill_sync_receipts"
CREATE UNIQUE INDEX "skill_sync_receipts_project_skill_user_host_provider_key" ON "skill_sync_receipts" ("project_id", "skill_id", "user_id", "hostname", "provider");
-- Create index "skill_sync_receipts_skill_id_idx" to table: "skill_sync_receipts"
CREATE INDEX "skill_sync_receipts_skill_id_idx" ON "skill_sync_receipts" ("skill_id");
-- Create index "skill_sync_receipts_skill_version_id_idx" to table: "skill_sync_receipts"
CREATE INDEX "skill_sync_receipts_skill_version_id_idx" ON "skill_sync_receipts" ("skill_version_id");
