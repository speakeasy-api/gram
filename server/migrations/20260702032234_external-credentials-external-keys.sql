-- Create "external_credentials" table
CREATE TABLE "external_credentials" (
  "id" uuid NOT NULL DEFAULT generate_uuidv7(),
  "organization_id" text NULL,
  "project_id" uuid NULL,
  "provider" text NOT NULL,
  "name" text NOT NULL,
  "created_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "updated_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "deleted_at" timestamptz NULL,
  "deleted" boolean NOT NULL GENERATED ALWAYS AS (deleted_at IS NOT NULL) STORED,
  PRIMARY KEY ("id"),
  CONSTRAINT "external_credentials_id_provider_key" UNIQUE ("id", "provider"),
  CONSTRAINT "external_credentials_organization_id_fkey" FOREIGN KEY ("organization_id") REFERENCES "organization_metadata" ("id") ON UPDATE NO ACTION ON DELETE CASCADE,
  CONSTRAINT "external_credentials_project_id_fkey" FOREIGN KEY ("project_id") REFERENCES "projects" ("id") ON UPDATE NO ACTION ON DELETE CASCADE,
  CONSTRAINT "external_credentials_provider_check" CHECK (provider = ANY (ARRAY['aws_iam'::text, 'gcp_iam'::text]))
);
-- Create index "external_credentials_organization_id_idx" to table: "external_credentials"
CREATE INDEX "external_credentials_organization_id_idx" ON "external_credentials" ("organization_id") WHERE (deleted IS FALSE);
-- Create "aws_iam_credentials" table
CREATE TABLE "aws_iam_credentials" (
  "external_credential_id" uuid NOT NULL,
  "external_credentials_provider" text NOT NULL DEFAULT 'aws_iam',
  "assume_role_arn" text NULL,
  "external_id" text NULL,
  "oidc_audience" text NULL,
  "oidc_subject" text NULL,
  "sts_region" text NULL,
  "created_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "updated_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  PRIMARY KEY ("external_credential_id"),
  CONSTRAINT "aws_iam_credentials_fkey" FOREIGN KEY ("external_credential_id", "external_credentials_provider") REFERENCES "external_credentials" ("id", "provider") ON UPDATE NO ACTION ON DELETE CASCADE,
  CONSTRAINT "aws_iam_credentials_auth_exclusive_check" CHECK (num_nonnulls(external_id, oidc_audience) <= 1),
  CONSTRAINT "aws_iam_credentials_external_credentials_provider_check" CHECK (external_credentials_provider = 'aws_iam'::text)
);
-- Create "external_keys" table
CREATE TABLE "external_keys" (
  "id" uuid NOT NULL DEFAULT generate_uuidv7(),
  "organization_id" text NULL,
  "project_id" uuid NULL,
  "external_credential_id" uuid NOT NULL,
  "provider" text NOT NULL,
  "algorithm" text NOT NULL,
  "name" text NOT NULL,
  "customer_grant_reference" text NULL,
  "created_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "updated_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "deleted_at" timestamptz NULL,
  "deleted" boolean NOT NULL GENERATED ALWAYS AS (deleted_at IS NOT NULL) STORED,
  PRIMARY KEY ("id"),
  CONSTRAINT "external_keys_id_provider_key" UNIQUE ("id", "provider"),
  CONSTRAINT "external_keys_external_credential_id_fkey" FOREIGN KEY ("external_credential_id") REFERENCES "external_credentials" ("id") ON UPDATE NO ACTION ON DELETE NO ACTION,
  CONSTRAINT "external_keys_organization_id_fkey" FOREIGN KEY ("organization_id") REFERENCES "organization_metadata" ("id") ON UPDATE NO ACTION ON DELETE CASCADE,
  CONSTRAINT "external_keys_project_id_fkey" FOREIGN KEY ("project_id") REFERENCES "projects" ("id") ON UPDATE NO ACTION ON DELETE CASCADE,
  CONSTRAINT "external_keys_provider_check" CHECK (provider = ANY (ARRAY['aws_kms'::text, 'gcp_kms'::text]))
);
-- Create index "external_keys_external_credential_id_idx" to table: "external_keys"
CREATE INDEX "external_keys_external_credential_id_idx" ON "external_keys" ("external_credential_id") WHERE (deleted IS FALSE);
-- Create index "external_keys_organization_id_idx" to table: "external_keys"
CREATE INDEX "external_keys_organization_id_idx" ON "external_keys" ("organization_id") WHERE (deleted IS FALSE);
-- Create "aws_kms_keys" table
CREATE TABLE "aws_kms_keys" (
  "external_key_id" uuid NOT NULL,
  "external_keys_provider" text NOT NULL DEFAULT 'aws_kms',
  "key_arn" text NOT NULL,
  "created_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "updated_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  PRIMARY KEY ("external_key_id"),
  CONSTRAINT "aws_kms_keys_fkey" FOREIGN KEY ("external_key_id", "external_keys_provider") REFERENCES "external_keys" ("id", "provider") ON UPDATE NO ACTION ON DELETE CASCADE,
  CONSTRAINT "aws_kms_keys_external_keys_provider_check" CHECK (external_keys_provider = 'aws_kms'::text)
);
-- Create "gcp_iam_credentials" table
CREATE TABLE "gcp_iam_credentials" (
  "external_credential_id" uuid NOT NULL,
  "external_credentials_provider" text NOT NULL DEFAULT 'gcp_iam',
  "impersonate_service_account" text NULL,
  "wif_pool_id" text NULL,
  "wif_provider_id" text NULL,
  "wif_project_number" text NULL,
  "created_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "updated_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  PRIMARY KEY ("external_credential_id"),
  CONSTRAINT "gcp_iam_credentials_fkey" FOREIGN KEY ("external_credential_id", "external_credentials_provider") REFERENCES "external_credentials" ("id", "provider") ON UPDATE NO ACTION ON DELETE CASCADE,
  CONSTRAINT "gcp_iam_credentials_external_credentials_provider_check" CHECK (external_credentials_provider = 'gcp_iam'::text),
  CONSTRAINT "gcp_iam_credentials_wif_complete_check" CHECK (num_nonnulls(wif_pool_id, wif_provider_id, wif_project_number) = ANY (ARRAY[0, 3]))
);
-- Create "gcp_kms_keys" table
CREATE TABLE "gcp_kms_keys" (
  "external_key_id" uuid NOT NULL,
  "external_keys_provider" text NOT NULL DEFAULT 'gcp_kms',
  "resource_name" text NOT NULL,
  "created_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "updated_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  PRIMARY KEY ("external_key_id"),
  CONSTRAINT "gcp_kms_keys_fkey" FOREIGN KEY ("external_key_id", "external_keys_provider") REFERENCES "external_keys" ("id", "provider") ON UPDATE NO ACTION ON DELETE CASCADE,
  CONSTRAINT "gcp_kms_keys_external_keys_provider_check" CHECK (external_keys_provider = 'gcp_kms'::text)
);
