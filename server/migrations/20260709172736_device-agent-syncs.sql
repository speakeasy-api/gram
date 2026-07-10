-- Create "device_agent_syncs" table
CREATE TABLE "device_agent_syncs" (
  "id" uuid NOT NULL DEFAULT generate_uuidv7(),
  "organization_id" text NOT NULL,
  "email" text NOT NULL,
  "first_seen_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "last_seen_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "created_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "updated_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  PRIMARY KEY ("id"),
  CONSTRAINT "device_agent_syncs_org_email_key" UNIQUE ("organization_id", "email"),
  CONSTRAINT "device_agent_syncs_organization_id_fkey" FOREIGN KEY ("organization_id") REFERENCES "organization_metadata" ("id") ON UPDATE NO ACTION ON DELETE CASCADE
);
