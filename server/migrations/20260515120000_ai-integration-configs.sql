-- Create "ai_integration_configs" table
CREATE TABLE "ai_integration_configs" (
  "created_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "deleted_at" timestamptz NULL,
  "updated_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "organization_id" text NOT NULL,
  "provider" text NOT NULL,
  "project_id" uuid NOT NULL,
  "api_key_encrypted" text NOT NULL,
  "enabled" boolean NOT NULL DEFAULT true,
  "id" uuid NOT NULL DEFAULT generate_uuidv7(),
  "deleted" boolean NOT NULL GENERATED ALWAYS AS (deleted_at IS NOT NULL) STORED,
  PRIMARY KEY ("id"),
  CONSTRAINT "ai_integration_configs_project_id_fkey" FOREIGN KEY ("project_id") REFERENCES "projects" ("id") ON DELETE CASCADE
);

-- Create index "ai_integration_configs_org_provider_key" to table: "ai_integration_configs"
CREATE UNIQUE INDEX "ai_integration_configs_org_provider_key" ON "ai_integration_configs" ("organization_id", "provider") WHERE (deleted IS FALSE);

-- Create "ai_integration_syncs" table
CREATE TABLE "ai_integration_syncs" (
  "created_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "updated_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "ai_integration_config_id" uuid NOT NULL,
  "last_polled_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "id" uuid NOT NULL DEFAULT generate_uuidv7(),
  PRIMARY KEY ("id"),
  CONSTRAINT "ai_integration_syncs_config_id_fkey" FOREIGN KEY ("ai_integration_config_id") REFERENCES "ai_integration_configs" ("id") ON DELETE CASCADE
);

-- Create index "ai_integration_syncs_config_id_key" to table: "ai_integration_syncs"
CREATE UNIQUE INDEX "ai_integration_syncs_config_id_key" ON "ai_integration_syncs" ("ai_integration_config_id");
