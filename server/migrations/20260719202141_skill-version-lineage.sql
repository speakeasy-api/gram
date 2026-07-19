-- atlas:txmode none

-- Modify "skill_versions" table
ALTER TABLE "skill_versions" ADD COLUMN "derived_from_version_id" uuid NULL, ADD CONSTRAINT "skill_versions_derived_from_version_id_fkey" FOREIGN KEY ("derived_from_version_id") REFERENCES "skill_versions" ("id") ON UPDATE NO ACTION ON DELETE SET NULL;
-- Create index "skill_versions_derived_from_version_id_idx" to table: "skill_versions"
CREATE INDEX CONCURRENTLY "skill_versions_derived_from_version_id_idx" ON "skill_versions" ("derived_from_version_id") WHERE (derived_from_version_id IS NOT NULL);
