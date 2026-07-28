-- atlas:txmode none

-- Create index "external_keys_organization_id_id_key" to table: "external_keys"
CREATE UNIQUE INDEX CONCURRENTLY "external_keys_organization_id_id_key" ON "external_keys" ("organization_id", "id");
-- Create "json_web_key_sets" table
CREATE TABLE "json_web_key_sets" (
  "id" uuid NOT NULL DEFAULT generate_uuidv7(),
  "organization_id" text NOT NULL,
  "project_id" uuid NULL,
  "external_key_id" uuid NOT NULL,
  "name" text NOT NULL,
  "created_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "updated_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "deleted_at" timestamptz NULL,
  "deleted" boolean NOT NULL GENERATED ALWAYS AS (deleted_at IS NOT NULL) STORED,
  PRIMARY KEY ("id"),
  CONSTRAINT "json_web_key_sets_external_key_tenant_fkey" FOREIGN KEY ("organization_id", "external_key_id") REFERENCES "external_keys" ("organization_id", "id") ON UPDATE NO ACTION ON DELETE NO ACTION,
  CONSTRAINT "json_web_key_sets_organization_id_fkey" FOREIGN KEY ("organization_id") REFERENCES "organization_metadata" ("id") ON UPDATE NO ACTION ON DELETE CASCADE,
  CONSTRAINT "json_web_key_sets_project_id_fkey" FOREIGN KEY ("project_id") REFERENCES "projects" ("id") ON UPDATE NO ACTION ON DELETE CASCADE
);
-- Create index "json_web_key_sets_organization_id_id_key" to table: "json_web_key_sets"
CREATE UNIQUE INDEX "json_web_key_sets_organization_id_id_key" ON "json_web_key_sets" ("organization_id", "id");
-- Create "json_web_keys" table
CREATE TABLE "json_web_keys" (
  "id" uuid NOT NULL DEFAULT generate_uuidv7(),
  "organization_id" text NOT NULL,
  "project_id" uuid NULL,
  "json_web_key_set_id" uuid NOT NULL,
  "external_key_id" uuid NOT NULL,
  "external_key_version" text NULL,
  "state" text NOT NULL,
  "kid" text NOT NULL,
  "public_jwk" jsonb NOT NULL,
  "activated_at" timestamptz NULL,
  "retired_at" timestamptz NULL,
  "revoked_at" timestamptz NULL,
  "created_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "updated_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "deleted_at" timestamptz NULL,
  "deleted" boolean NOT NULL GENERATED ALWAYS AS (deleted_at IS NOT NULL) STORED,
  PRIMARY KEY ("id"),
  CONSTRAINT "json_web_keys_external_key_tenant_fkey" FOREIGN KEY ("organization_id", "external_key_id") REFERENCES "external_keys" ("organization_id", "id") ON UPDATE NO ACTION ON DELETE NO ACTION,
  CONSTRAINT "json_web_keys_project_id_fkey" FOREIGN KEY ("project_id") REFERENCES "projects" ("id") ON UPDATE NO ACTION ON DELETE CASCADE,
  CONSTRAINT "json_web_keys_set_tenant_fkey" FOREIGN KEY ("organization_id", "json_web_key_set_id") REFERENCES "json_web_key_sets" ("organization_id", "id") ON UPDATE NO ACTION ON DELETE CASCADE
);
-- Create index "json_web_keys_one_active_idx" to table: "json_web_keys"
CREATE UNIQUE INDEX "json_web_keys_one_active_idx" ON "json_web_keys" ("json_web_key_set_id") WHERE ((state = 'active'::text) AND (deleted IS FALSE));
-- Create index "json_web_keys_set_kid_idx" to table: "json_web_keys"
CREATE UNIQUE INDEX "json_web_keys_set_kid_idx" ON "json_web_keys" ("json_web_key_set_id", "kid") WHERE (deleted IS FALSE);
-- Create index "json_web_keys_set_tenant_idx" to table: "json_web_keys"
CREATE INDEX "json_web_keys_set_tenant_idx" ON "json_web_keys" ("organization_id", "json_web_key_set_id");
