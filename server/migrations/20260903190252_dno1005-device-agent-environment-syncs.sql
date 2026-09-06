-- Create "device_agent_environment_syncs" table
CREATE TABLE "device_agent_environment_syncs" (
  "id" uuid NOT NULL DEFAULT generate_uuidv7(),
  "organization_id" text NOT NULL,
  "email" text NOT NULL,
  "environment" text NOT NULL,
  "hostname" text NULL,
  "first_seen_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "last_seen_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "created_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "updated_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  PRIMARY KEY ("id"),
  CONSTRAINT "device_agent_environment_syncs_organization_id_fkey" FOREIGN KEY ("organization_id") REFERENCES "organization_metadata" ("id") ON UPDATE NO ACTION ON DELETE CASCADE
);
-- Create index "device_agent_environment_syncs_org_lower_email_env_key" to table: "device_agent_environment_syncs"
CREATE UNIQUE INDEX "device_agent_environment_syncs_org_lower_email_env_key" ON "device_agent_environment_syncs" ("organization_id", (lower(email)), "environment");
