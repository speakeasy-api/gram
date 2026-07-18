-- Modify "skill_versions" table
ALTER TABLE "skill_versions" ADD COLUMN "origin" text NOT NULL DEFAULT 'manual';
