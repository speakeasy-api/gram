-- Modify "toolsets" table
ALTER TABLE "toolsets" DROP CONSTRAINT "toolsets_project_id_name_key", ADD COLUMN "slug" text NOT NULL, ADD CONSTRAINT "toolsets_project_id_slug_key" UNIQUE ("project_id", "slug");
