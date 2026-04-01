-- Modify "toolsets" table
ALTER TABLE "toolsets" ADD COLUMN "catalog_published" boolean NOT NULL DEFAULT false, ADD COLUMN "catalog_published_by" text NULL, ADD COLUMN "catalog_published_at" timestamptz NULL;
