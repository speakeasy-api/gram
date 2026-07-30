-- atlas:txmode none

-- Create index "mdm_devices_organization_id_lower_serial_number_idx" to table: "mdm_devices"
CREATE INDEX CONCURRENTLY "mdm_devices_organization_id_lower_serial_number_idx" ON "mdm_devices" ("organization_id", (lower(serial_number))) WHERE (missing_since IS NULL);
-- Create "device_agent_device_syncs" table
CREATE TABLE "device_agent_device_syncs" (
  "id" uuid NOT NULL DEFAULT generate_uuidv7(),
  "organization_id" text NOT NULL,
  "serial_number" text NOT NULL,
  "email" text NOT NULL,
  "hostname" text NULL,
  "first_seen_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "last_seen_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "created_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "updated_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  PRIMARY KEY ("id"),
  CONSTRAINT "device_agent_device_syncs_organization_id_fkey" FOREIGN KEY ("organization_id") REFERENCES "organization_metadata" ("id") ON UPDATE NO ACTION ON DELETE CASCADE
);
-- Create index "device_agent_device_syncs_org_lower_serial_key" to table: "device_agent_device_syncs"
CREATE UNIQUE INDEX "device_agent_device_syncs_org_lower_serial_key" ON "device_agent_device_syncs" ("organization_id", (lower(serial_number)));
