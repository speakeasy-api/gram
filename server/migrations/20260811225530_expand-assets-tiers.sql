-- atlas:txmode none

-- Modify "assets" table
ALTER TABLE "assets" ALTER COLUMN "project_id" DROP NOT NULL, ADD COLUMN "organization_id" text NULL, ADD CONSTRAINT "assets_organization_id_fkey" FOREIGN KEY ("organization_id") REFERENCES "organization_metadata" ("id") ON UPDATE NO ACTION ON DELETE CASCADE NOT VALID;
ALTER TABLE "assets" VALIDATE CONSTRAINT "assets_organization_id_fkey";
