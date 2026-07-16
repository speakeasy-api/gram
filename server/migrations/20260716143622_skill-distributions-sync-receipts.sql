-- atlas:txmode none

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
  "audience" text[] NULL,
  "channel" text NOT NULL DEFAULT 'plugin',
  "created_by_user_id" text NOT NULL,
  "revoked_at" timestamptz NULL,
  "created_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "updated_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  PRIMARY KEY ("id"),
  CONSTRAINT "skill_distributions_project_id_fkey" FOREIGN KEY ("project_id") REFERENCES "projects" ("id") ON UPDATE NO ACTION ON DELETE CASCADE,
  CONSTRAINT "skill_distributions_project_id_skill_id_fkey" FOREIGN KEY ("project_id", "skill_id") REFERENCES "skills" ("project_id", "id") ON UPDATE NO ACTION ON DELETE CASCADE,
  CONSTRAINT "skill_distributions_skill_id_pinned_version_id_fkey" FOREIGN KEY ("skill_id", "pinned_version_id") REFERENCES "skill_versions" ("skill_id", "id") ON UPDATE NO ACTION ON DELETE NO ACTION,
  CONSTRAINT "skill_distributions_audience_check" CHECK ((audience IS NULL) OR (cardinality(audience) > 0))
);
-- Create index "skill_distributions_project_id_idx" to table: "skill_distributions"
CREATE INDEX "skill_distributions_project_id_idx" ON "skill_distributions" ("project_id");
-- Create index "skill_distributions_project_id_skill_id_channel_key" to table: "skill_distributions"
CREATE UNIQUE INDEX "skill_distributions_project_id_skill_id_channel_key" ON "skill_distributions" ("project_id", "skill_id", "channel") WHERE (revoked_at IS NULL);
-- Create index "skill_distributions_skill_id_pinned_version_id_idx" to table: "skill_distributions"
CREATE INDEX "skill_distributions_skill_id_pinned_version_id_idx" ON "skill_distributions" ("skill_id", "pinned_version_id");
-- Create "skill_sync_receipts" table
CREATE TABLE "skill_sync_receipts" (
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
  PRIMARY KEY ("project_id", "skill_id", "user_id", "hostname", "provider"),
  CONSTRAINT "skill_sync_receipts_project_id_fkey" FOREIGN KEY ("project_id") REFERENCES "projects" ("id") ON UPDATE NO ACTION ON DELETE CASCADE,
  CONSTRAINT "skill_sync_receipts_project_id_skill_id_fkey" FOREIGN KEY ("project_id", "skill_id") REFERENCES "skills" ("project_id", "id") ON UPDATE NO ACTION ON DELETE CASCADE,
  CONSTRAINT "skill_sync_receipts_skill_id_skill_version_id_fkey" FOREIGN KEY ("skill_id", "skill_version_id") REFERENCES "skill_versions" ("skill_id", "id") ON UPDATE NO ACTION ON DELETE NO ACTION
);
-- Create index "skill_sync_receipts_project_id_skill_version_id_idx" to table: "skill_sync_receipts"
CREATE INDEX "skill_sync_receipts_project_id_skill_version_id_idx" ON "skill_sync_receipts" ("project_id", "skill_version_id");
-- Create index "skill_sync_receipts_skill_id_skill_version_id_idx" to table: "skill_sync_receipts"
CREATE INDEX "skill_sync_receipts_skill_id_skill_version_id_idx" ON "skill_sync_receipts" ("skill_id", "skill_version_id");
