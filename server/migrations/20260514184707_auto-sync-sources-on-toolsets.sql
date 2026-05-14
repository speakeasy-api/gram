-- Modify "toolsets" table
ALTER TABLE "toolsets" ADD COLUMN "auto_sync_sources" text[] NOT NULL DEFAULT ARRAY[]::text[];
