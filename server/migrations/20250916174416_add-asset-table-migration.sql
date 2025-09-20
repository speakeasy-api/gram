-- Modify "deployment_logs" table
ALTER TABLE "deployment_logs" ADD COLUMN "attachment_id" uuid NULL, ADD COLUMN "attachment_type" text NULL;
