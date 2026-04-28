-- Modify "deployments_functions" table
ALTER TABLE "deployments_functions" ADD COLUMN "memory_mib" integer NULL, ADD COLUMN "scale" integer NULL;
