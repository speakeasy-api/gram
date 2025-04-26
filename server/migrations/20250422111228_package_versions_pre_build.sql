-- Drop index "package_versions_package_id_major_minor_patch_suffix_key" from table: "package_versions"
DROP INDEX "package_versions_package_id_major_minor_patch_suffix_key";
-- Rename a column from "suffix" to "build"
ALTER TABLE "package_versions" RENAME COLUMN "suffix" TO "build";
-- Modify "package_versions" table
ALTER TABLE "package_versions" ADD COLUMN "prerelease" character varying(20) NULL;
-- Create index "package_versions_package_id_major_minor_patch_prerelease_build_" to table: "package_versions"
CREATE UNIQUE INDEX "package_versions_package_id_major_minor_patch_prerelease_build_" ON "package_versions" ("package_id", "major", "minor", "patch", "prerelease", "build") WHERE (deleted IS FALSE);
