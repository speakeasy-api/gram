-- Create "skill_version_origins" table
CREATE TABLE "skill_version_origins" (
  "skill_version_id" uuid NOT NULL,
  "skill_id" uuid NOT NULL,
  "project_id" uuid NOT NULL,
  "origin" text NOT NULL,
  "created_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  PRIMARY KEY ("skill_version_id"),
  CONSTRAINT "skill_version_origins_project_id_skill_id_fkey" FOREIGN KEY ("project_id", "skill_id") REFERENCES "skills" ("project_id", "id") ON UPDATE NO ACTION ON DELETE CASCADE,
  CONSTRAINT "skill_version_origins_skill_id_skill_version_id_fkey" FOREIGN KEY ("skill_id", "skill_version_id") REFERENCES "skill_versions" ("skill_id", "id") ON UPDATE NO ACTION ON DELETE CASCADE
);
-- Create index "skill_version_origins_project_id_skill_id_idx" to table: "skill_version_origins"
CREATE INDEX "skill_version_origins_project_id_skill_id_idx" ON "skill_version_origins" ("project_id", "skill_id");
