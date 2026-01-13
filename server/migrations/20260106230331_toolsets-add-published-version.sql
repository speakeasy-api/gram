-- Modify "toolsets" table
ALTER TABLE "toolsets" ADD COLUMN "published_toolset_version_id" uuid NULL, ADD CONSTRAINT "toolsets_published_toolset_version_id_fkey" FOREIGN KEY ("published_toolset_version_id") REFERENCES "toolset_versions" ("id") ON UPDATE NO ACTION ON DELETE SET NULL;
