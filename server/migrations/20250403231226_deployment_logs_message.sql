-- Modify "deployment_logs" table
ALTER TABLE "deployment_logs" DROP CONSTRAINT "deployment_logs_check", DROP COLUMN "tooltemplate_id", DROP COLUMN "tooltemplate_type", DROP COLUMN "collection_id", ADD COLUMN "message" text NOT NULL;
