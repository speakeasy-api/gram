-- Create "device_agent_configurations" table
CREATE TABLE "device_agent_configurations" (
  "organization_id" text NOT NULL,
  "schema_version" integer NOT NULL DEFAULT 1,
  "config" jsonb NOT NULL DEFAULT '{}',
  "created_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "updated_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  PRIMARY KEY ("organization_id"),
  CONSTRAINT "device_agent_configurations_organization_id_fkey" FOREIGN KEY ("organization_id") REFERENCES "organization_metadata" ("id") ON UPDATE NO ACTION ON DELETE CASCADE
);
