-- Modify "deployments" table
ALTER TABLE "deployments" ADD COLUMN "cloned_from" uuid NULL;
