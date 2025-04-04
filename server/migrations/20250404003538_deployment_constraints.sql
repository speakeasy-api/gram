-- Modify "deployments" table
ALTER TABLE "deployments" DROP CONSTRAINT "deployments_project_id_seq_key", ADD CONSTRAINT "deployments_seq_key" UNIQUE ("seq");
-- Modify "deployments_openapiv3_assets" table
ALTER TABLE "deployments_openapiv3_assets" ADD CONSTRAINT "deployments_openapiv3_documents_deployment_id_slug_key" UNIQUE ("deployment_id", "slug");
