-- Create "skill_version_lineages" table
CREATE TABLE "skill_version_lineages" (
  "skill_version_id" uuid NOT NULL,
  "skill_id" uuid NOT NULL,
  "derived_from_version_id" uuid NOT NULL,
  PRIMARY KEY ("skill_version_id"),
  CONSTRAINT "skill_version_lineages_skill_id_derived_from_version_id_fkey" FOREIGN KEY ("skill_id", "derived_from_version_id") REFERENCES "skill_versions" ("skill_id", "id") ON UPDATE NO ACTION ON DELETE NO ACTION,
  CONSTRAINT "skill_version_lineages_skill_id_skill_version_id_fkey" FOREIGN KEY ("skill_id", "skill_version_id") REFERENCES "skill_versions" ("skill_id", "id") ON UPDATE NO ACTION ON DELETE CASCADE
);
-- Create index "skill_version_lineages_skill_id_derived_from_version_id_idx" to table: "skill_version_lineages"
CREATE INDEX "skill_version_lineages_skill_id_derived_from_version_id_idx" ON "skill_version_lineages" ("skill_id", "derived_from_version_id");
