-- Modify "gcp_iam_credentials" table
ALTER TABLE "gcp_iam_credentials" ADD COLUMN "skip_project_verification" boolean NOT NULL DEFAULT false;
