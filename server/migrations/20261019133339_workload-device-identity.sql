-- Create "workload_device_join_tokens" table
CREATE TABLE "workload_device_join_tokens" (
  "id" uuid NOT NULL DEFAULT generate_uuidv7(),
  "organization_id" text NOT NULL,
  "token_hash" text NOT NULL,
  "agent_id" uuid NOT NULL,
  "created_by_user_id" text NOT NULL,
  "expires_at" timestamptz NOT NULL,
  "consumed_at" timestamptz NULL,
  "consumed_by_subject" text NULL,
  "created_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "updated_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "deleted_at" timestamptz NULL,
  "deleted" boolean NOT NULL GENERATED ALWAYS AS (deleted_at IS NOT NULL) STORED,
  PRIMARY KEY ("id"),
  CONSTRAINT "workload_device_join_tokens_agent_fkey" FOREIGN KEY ("organization_id", "agent_id") REFERENCES "agents" ("organization_id", "id") ON UPDATE NO ACTION ON DELETE CASCADE,
  CONSTRAINT "workload_device_join_tokens_organization_id_fkey" FOREIGN KEY ("organization_id") REFERENCES "organization_metadata" ("id") ON UPDATE NO ACTION ON DELETE CASCADE,
  CONSTRAINT "workload_device_join_tokens_token_hash_check" CHECK (token_hash <> ''::text)
);
-- Create index "workload_device_join_tokens_agent_idx" to table: "workload_device_join_tokens"
CREATE INDEX "workload_device_join_tokens_agent_idx" ON "workload_device_join_tokens" ("organization_id", "agent_id");
-- Create index "workload_device_join_tokens_token_hash_key" to table: "workload_device_join_tokens"
CREATE UNIQUE INDEX "workload_device_join_tokens_token_hash_key" ON "workload_device_join_tokens" ("token_hash");
-- Set comment to column: "id" on table: "workload_device_join_tokens"
COMMENT ON COLUMN "workload_device_join_tokens"."id" IS '@access: confidential';
-- Set comment to column: "organization_id" on table: "workload_device_join_tokens"
COMMENT ON COLUMN "workload_device_join_tokens"."organization_id" IS '@access: confidential';
-- Set comment to column: "token_hash" on table: "workload_device_join_tokens"
COMMENT ON COLUMN "workload_device_join_tokens"."token_hash" IS '@access: secret-restricted';
-- Set comment to column: "agent_id" on table: "workload_device_join_tokens"
COMMENT ON COLUMN "workload_device_join_tokens"."agent_id" IS '@access: confidential';
-- Set comment to column: "created_by_user_id" on table: "workload_device_join_tokens"
COMMENT ON COLUMN "workload_device_join_tokens"."created_by_user_id" IS '@access: confidential';
-- Set comment to column: "expires_at" on table: "workload_device_join_tokens"
COMMENT ON COLUMN "workload_device_join_tokens"."expires_at" IS '@access: confidential';
-- Set comment to column: "consumed_at" on table: "workload_device_join_tokens"
COMMENT ON COLUMN "workload_device_join_tokens"."consumed_at" IS '@access: confidential';
-- Set comment to column: "consumed_by_subject" on table: "workload_device_join_tokens"
COMMENT ON COLUMN "workload_device_join_tokens"."consumed_by_subject" IS '@access: restricted';
-- Set comment to column: "created_at" on table: "workload_device_join_tokens"
COMMENT ON COLUMN "workload_device_join_tokens"."created_at" IS '@access: confidential';
-- Set comment to column: "updated_at" on table: "workload_device_join_tokens"
COMMENT ON COLUMN "workload_device_join_tokens"."updated_at" IS '@access: confidential';
-- Set comment to column: "deleted_at" on table: "workload_device_join_tokens"
COMMENT ON COLUMN "workload_device_join_tokens"."deleted_at" IS '@access: confidential';
-- Set comment to column: "deleted" on table: "workload_device_join_tokens"
COMMENT ON COLUMN "workload_device_join_tokens"."deleted" IS '@access: confidential';
-- Modify "workload_issuers" table
ALTER TABLE "workload_issuers" ADD COLUMN "key_source" text NULL;
-- Set comment to column: "key_source" on table: "workload_issuers"
COMMENT ON COLUMN "workload_issuers"."key_source" IS '@access: confidential';
-- Create "workload_device_keys" table
CREATE TABLE "workload_device_keys" (
  "id" uuid NOT NULL DEFAULT generate_uuidv7(),
  "organization_id" text NOT NULL,
  "workload_issuer_id" uuid NOT NULL,
  "subject" text NOT NULL,
  "public_jwk" jsonb NOT NULL,
  "thumbprint" text NOT NULL,
  "hostname" text NULL,
  "serial" text NULL,
  "created_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "updated_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "deleted_at" timestamptz NULL,
  "deleted" boolean NOT NULL GENERATED ALWAYS AS (deleted_at IS NOT NULL) STORED,
  PRIMARY KEY ("id"),
  CONSTRAINT "workload_device_keys_organization_id_fkey" FOREIGN KEY ("organization_id") REFERENCES "organization_metadata" ("id") ON UPDATE NO ACTION ON DELETE CASCADE,
  CONSTRAINT "workload_device_keys_workload_issuer_fkey" FOREIGN KEY ("organization_id", "workload_issuer_id") REFERENCES "workload_issuers" ("organization_id", "id") ON UPDATE NO ACTION ON DELETE CASCADE,
  CONSTRAINT "workload_device_keys_subject_check" CHECK (subject <> ''::text),
  CONSTRAINT "workload_device_keys_thumbprint_check" CHECK (thumbprint <> ''::text)
);
-- Create index "workload_device_keys_subject_key" to table: "workload_device_keys"
CREATE UNIQUE INDEX "workload_device_keys_subject_key" ON "workload_device_keys" ("organization_id", "workload_issuer_id", "subject") WHERE (deleted IS FALSE);
-- Create index "workload_device_keys_thumbprint_key" to table: "workload_device_keys"
CREATE UNIQUE INDEX "workload_device_keys_thumbprint_key" ON "workload_device_keys" ("organization_id", "workload_issuer_id", "thumbprint") WHERE (deleted IS FALSE);
-- Create index "workload_device_keys_workload_issuer_idx" to table: "workload_device_keys"
CREATE INDEX "workload_device_keys_workload_issuer_idx" ON "workload_device_keys" ("organization_id", "workload_issuer_id");
-- Set comment to column: "id" on table: "workload_device_keys"
COMMENT ON COLUMN "workload_device_keys"."id" IS '@access: confidential';
-- Set comment to column: "organization_id" on table: "workload_device_keys"
COMMENT ON COLUMN "workload_device_keys"."organization_id" IS '@access: confidential';
-- Set comment to column: "workload_issuer_id" on table: "workload_device_keys"
COMMENT ON COLUMN "workload_device_keys"."workload_issuer_id" IS '@access: confidential';
-- Set comment to column: "subject" on table: "workload_device_keys"
COMMENT ON COLUMN "workload_device_keys"."subject" IS '@access: restricted';
-- Set comment to column: "public_jwk" on table: "workload_device_keys"
COMMENT ON COLUMN "workload_device_keys"."public_jwk" IS '@access: confidential';
-- Set comment to column: "thumbprint" on table: "workload_device_keys"
COMMENT ON COLUMN "workload_device_keys"."thumbprint" IS '@access: confidential';
-- Set comment to column: "hostname" on table: "workload_device_keys"
COMMENT ON COLUMN "workload_device_keys"."hostname" IS '@access: confidential-pii';
-- Set comment to column: "serial" on table: "workload_device_keys"
COMMENT ON COLUMN "workload_device_keys"."serial" IS '@access: confidential-pii';
-- Set comment to column: "created_at" on table: "workload_device_keys"
COMMENT ON COLUMN "workload_device_keys"."created_at" IS '@access: confidential';
-- Set comment to column: "updated_at" on table: "workload_device_keys"
COMMENT ON COLUMN "workload_device_keys"."updated_at" IS '@access: confidential';
-- Set comment to column: "deleted_at" on table: "workload_device_keys"
COMMENT ON COLUMN "workload_device_keys"."deleted_at" IS '@access: confidential';
-- Set comment to column: "deleted" on table: "workload_device_keys"
COMMENT ON COLUMN "workload_device_keys"."deleted" IS '@access: confidential';
