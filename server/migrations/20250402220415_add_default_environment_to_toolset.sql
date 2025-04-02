-- Modify "toolsets" table
ALTER TABLE "toolsets" ADD COLUMN "default_environment_id" uuid NULL, ADD CONSTRAINT "toolsets_default_environment_id_fkey" FOREIGN KEY ("default_environment_id") REFERENCES "environments" ("id") ON UPDATE NO ACTION ON DELETE SET NULL;
