-- Modify "deployments_functions" table
ALTER TABLE "deployments_functions" ADD COLUMN "memory_mib_override" integer NULL, ADD COLUMN "scale_override" integer NULL;
