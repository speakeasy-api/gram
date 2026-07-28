-- Create "model_provider_keys" table
CREATE TABLE "model_provider_keys" (
  "id" uuid NOT NULL DEFAULT generate_uuidv7(),
  "organization_id" text NOT NULL,
  "project_id" uuid NOT NULL,
  "slot" text NOT NULL,
  "provider" text NOT NULL,
  "api_key_encrypted" text NOT NULL,
  "enabled" boolean NOT NULL DEFAULT true,
  "created_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "updated_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "deleted_at" timestamptz NULL,
  "deleted" boolean NOT NULL GENERATED ALWAYS AS (deleted_at IS NOT NULL) STORED,
  PRIMARY KEY ("id"),
  CONSTRAINT "model_provider_keys_organization_id_fkey" FOREIGN KEY ("organization_id") REFERENCES "organization_metadata" ("id") ON UPDATE NO ACTION ON DELETE CASCADE,
  CONSTRAINT "model_provider_keys_project_id_fkey" FOREIGN KEY ("project_id") REFERENCES "projects" ("id") ON UPDATE NO ACTION ON DELETE CASCADE
);
-- Create index "model_provider_keys_organization_id_idx" to table: "model_provider_keys"
CREATE INDEX "model_provider_keys_organization_id_idx" ON "model_provider_keys" ("organization_id");
-- Create index "model_provider_keys_project_id_idx" to table: "model_provider_keys"
CREATE INDEX "model_provider_keys_project_id_idx" ON "model_provider_keys" ("project_id");
-- Create index "model_provider_keys_project_id_slot_key" to table: "model_provider_keys"
CREATE UNIQUE INDEX "model_provider_keys_project_id_slot_key" ON "model_provider_keys" ("project_id", "slot") WHERE (deleted IS FALSE);
