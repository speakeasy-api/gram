-- Modify "deployments" table
ALTER TABLE "deployments" DROP CONSTRAINT "deployments_external_id_check", DROP CONSTRAINT "deployments_external_url_check", DROP CONSTRAINT "deployments_github_pr_check";
