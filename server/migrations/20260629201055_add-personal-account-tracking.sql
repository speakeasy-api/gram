-- Modify "chats" table
ALTER TABLE "chats" ADD COLUMN "user_account_id" uuid NULL;
-- Create "device_owners" table
CREATE TABLE "device_owners" (
  "id" uuid NOT NULL DEFAULT generate_uuidv7(),
  "organization_id" text NOT NULL,
  "provider" text NOT NULL DEFAULT 'anthropic',
  "device_id" text NOT NULL,
  "linked_user_id" text NULL,
  "first_seen_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "last_seen_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "created_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "updated_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "deleted_at" timestamptz NULL,
  "deleted" boolean NOT NULL GENERATED ALWAYS AS (deleted_at IS NOT NULL) STORED,
  PRIMARY KEY ("id"),
  CONSTRAINT "device_owners_organization_id_provider_device_id_key" UNIQUE ("organization_id", "provider", "device_id"),
  CONSTRAINT "device_owners_linked_user_id_fkey" FOREIGN KEY ("linked_user_id") REFERENCES "users" ("id") ON UPDATE NO ACTION ON DELETE SET NULL,
  CONSTRAINT "device_owners_organization_id_fkey" FOREIGN KEY ("organization_id") REFERENCES "organization_metadata" ("id") ON UPDATE NO ACTION ON DELETE CASCADE
);
-- Create "user_accounts" table
CREATE TABLE "user_accounts" (
  "id" uuid NOT NULL DEFAULT generate_uuidv7(),
  "organization_id" text NOT NULL,
  "user_id" text NULL,
  "provider" text NOT NULL DEFAULT 'anthropic',
  "external_org_id" text NULL,
  "external_account_uuid" text NOT NULL,
  "external_account_id" text NULL,
  "email" text NULL,
  "account_type" text NULL,
  "first_seen_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "last_seen_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "created_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "updated_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "deleted_at" timestamptz NULL,
  "deleted" boolean NOT NULL GENERATED ALWAYS AS (deleted_at IS NOT NULL) STORED,
  PRIMARY KEY ("id"),
  CONSTRAINT "user_accounts_org_provider_external_account_uuid_key" UNIQUE ("organization_id", "provider", "external_account_uuid"),
  CONSTRAINT "user_accounts_organization_id_fkey" FOREIGN KEY ("organization_id") REFERENCES "organization_metadata" ("id") ON UPDATE NO ACTION ON DELETE CASCADE,
  CONSTRAINT "user_accounts_user_id_fkey" FOREIGN KEY ("user_id") REFERENCES "users" ("id") ON UPDATE NO ACTION ON DELETE SET NULL,
  CONSTRAINT "user_accounts_account_type_check" CHECK (account_type = ANY (ARRAY['team'::text, 'personal'::text]))
);
-- Create index "user_accounts_organization_id_user_id_idx" to table: "user_accounts"
CREATE INDEX "user_accounts_organization_id_user_id_idx" ON "user_accounts" ("organization_id", "user_id");
