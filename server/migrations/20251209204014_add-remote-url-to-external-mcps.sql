-- Modify "deployments_external_mcps" table
ALTER TABLE "deployments_external_mcps" ADD CONSTRAINT "deployments_external_mcps_remote_url_check" CHECK (remote_url <> ''::text), ADD COLUMN "remote_url" text NOT NULL;
