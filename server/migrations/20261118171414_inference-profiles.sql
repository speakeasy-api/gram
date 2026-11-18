-- Create "inference_profiles" table
CREATE TABLE "inference_profiles" (
  "id" uuid NOT NULL DEFAULT generate_uuidv7(),
  "organization_id" text NOT NULL,
  "name" text NOT NULL,
  "provider" text NOT NULL,
  "settings" jsonb NOT NULL DEFAULT '{}',
  "created_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "updated_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "deleted_at" timestamptz NULL,
  "deleted" boolean NOT NULL GENERATED ALWAYS AS (deleted_at IS NOT NULL) STORED,
  PRIMARY KEY ("id"),
  CONSTRAINT "inference_profiles_organization_id_id_key" UNIQUE ("organization_id", "id"),
  CONSTRAINT "inference_profiles_organization_id_fkey" FOREIGN KEY ("organization_id") REFERENCES "organization_metadata" ("id") ON UPDATE NO ACTION ON DELETE CASCADE,
  CONSTRAINT "inference_profiles_name_check" CHECK ((name <> ''::text) AND (char_length(name) <= 100))
);
-- Create index "inference_profiles_organization_id_name_key" to table: "inference_profiles"
CREATE UNIQUE INDEX "inference_profiles_organization_id_name_key" ON "inference_profiles" ("organization_id", (lower(name))) WHERE (deleted IS FALSE);
-- Set comment to column: "id" on table: "inference_profiles"
COMMENT ON COLUMN "inference_profiles"."id" IS '@access: confidential';
-- Set comment to column: "organization_id" on table: "inference_profiles"
COMMENT ON COLUMN "inference_profiles"."organization_id" IS '@access: confidential';
-- Set comment to column: "name" on table: "inference_profiles"
COMMENT ON COLUMN "inference_profiles"."name" IS 'Administrator-chosen display name.
@access: opaque-confidential';
-- Set comment to column: "provider" on table: "inference_profiles"
COMMENT ON COLUMN "inference_profiles"."provider" IS '@access: confidential';
-- Set comment to column: "settings" on table: "inference_profiles"
COMMENT ON COLUMN "inference_profiles"."settings" IS 'Customer-configured cloud region, AWS profile name, GCP project id, gateway URL and model ids. Never a credential.
@access: restricted';
-- Set comment to column: "created_at" on table: "inference_profiles"
COMMENT ON COLUMN "inference_profiles"."created_at" IS '@access: confidential';
-- Set comment to column: "updated_at" on table: "inference_profiles"
COMMENT ON COLUMN "inference_profiles"."updated_at" IS '@access: confidential';
-- Set comment to column: "deleted_at" on table: "inference_profiles"
COMMENT ON COLUMN "inference_profiles"."deleted_at" IS '@access: confidential';
-- Set comment to column: "deleted" on table: "inference_profiles"
COMMENT ON COLUMN "inference_profiles"."deleted" IS '@access: confidential';
-- Create "inference_profile_rules" table
CREATE TABLE "inference_profile_rules" (
  "id" uuid NOT NULL DEFAULT generate_uuidv7(),
  "organization_id" text NOT NULL,
  "profile_id" uuid NOT NULL,
  "principal_urn" text NOT NULL,
  "harness" text NULL,
  "position" integer NOT NULL DEFAULT 0,
  "created_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "updated_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "deleted_at" timestamptz NULL,
  "deleted" boolean NOT NULL GENERATED ALWAYS AS (deleted_at IS NOT NULL) STORED,
  PRIMARY KEY ("id"),
  CONSTRAINT "inference_profile_rules_organization_id_fkey" FOREIGN KEY ("organization_id") REFERENCES "organization_metadata" ("id") ON UPDATE NO ACTION ON DELETE CASCADE,
  CONSTRAINT "inference_profile_rules_organization_id_profile_id_fkey" FOREIGN KEY ("organization_id", "profile_id") REFERENCES "inference_profiles" ("organization_id", "id") ON UPDATE NO ACTION ON DELETE NO ACTION,
  CONSTRAINT "inference_profile_rules_principal_urn_check" CHECK (principal_urn <> ''::text)
);
-- Create index "inference_profile_rules_org_principal_harness_key" to table: "inference_profile_rules"
CREATE UNIQUE INDEX "inference_profile_rules_org_principal_harness_key" ON "inference_profile_rules" ("organization_id", "principal_urn", "harness") NULLS NOT DISTINCT WHERE (deleted IS FALSE);
-- Create index "inference_profile_rules_organization_id_profile_id_idx" to table: "inference_profile_rules"
CREATE INDEX "inference_profile_rules_organization_id_profile_id_idx" ON "inference_profile_rules" ("organization_id", "profile_id");
-- Set comment to column: "id" on table: "inference_profile_rules"
COMMENT ON COLUMN "inference_profile_rules"."id" IS '@access: confidential';
-- Set comment to column: "organization_id" on table: "inference_profile_rules"
COMMENT ON COLUMN "inference_profile_rules"."organization_id" IS '@access: confidential';
-- Set comment to column: "profile_id" on table: "inference_profile_rules"
COMMENT ON COLUMN "inference_profile_rules"."profile_id" IS '@access: confidential';
-- Set comment to column: "principal_urn" on table: "inference_profile_rules"
COMMENT ON COLUMN "inference_profile_rules"."principal_urn" IS 'Principal the rule targets; an email principal carries a person''s address.
@access: confidential-pii';
-- Set comment to column: "harness" on table: "inference_profile_rules"
COMMENT ON COLUMN "inference_profile_rules"."harness" IS '@access: confidential';
-- Set comment to column: "position" on table: "inference_profile_rules"
COMMENT ON COLUMN "inference_profile_rules"."position" IS '@access: confidential';
-- Set comment to column: "created_at" on table: "inference_profile_rules"
COMMENT ON COLUMN "inference_profile_rules"."created_at" IS '@access: confidential';
-- Set comment to column: "updated_at" on table: "inference_profile_rules"
COMMENT ON COLUMN "inference_profile_rules"."updated_at" IS '@access: confidential';
-- Set comment to column: "deleted_at" on table: "inference_profile_rules"
COMMENT ON COLUMN "inference_profile_rules"."deleted_at" IS '@access: confidential';
-- Set comment to column: "deleted" on table: "inference_profile_rules"
COMMENT ON COLUMN "inference_profile_rules"."deleted" IS '@access: confidential';
