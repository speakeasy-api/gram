-- Modify "deployments" table
ALTER TABLE "deployments" ADD COLUMN "seq" bigserial NOT NULL, ADD CONSTRAINT "deployments_project_id_seq_key" UNIQUE ("project_id", "seq");
