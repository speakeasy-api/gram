-- Modify "toolsets" table
ALTER TABLE "toolsets" DROP COLUMN "default_environment_id", ADD COLUMN "default_environment_slug" text NULL;
