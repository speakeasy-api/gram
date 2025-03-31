-- Modify "projects" table
ALTER TABLE "projects" ADD COLUMN "name" text NOT NULL, ADD COLUMN "slug" text NOT NULL, ADD CONSTRAINT "projects_organization_id_slug_key" UNIQUE ("organization_id", "slug");
