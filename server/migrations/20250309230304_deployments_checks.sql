-- Modify "deployments" table
ALTER TABLE "deployments" ADD CONSTRAINT "deployments_external_id_check" CHECK ((external_id <> ''::text) AND (length(external_id) <= 80)), ADD CONSTRAINT "deployments_external_url_check" CHECK ((external_url <> ''::text) AND (length(external_url) <= 150)), DROP COLUMN "git_sha";
