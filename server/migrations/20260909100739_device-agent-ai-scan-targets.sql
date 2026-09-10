-- Create "device_agent_ai_scan_catalogs" table
CREATE TABLE "device_agent_ai_scan_catalogs" (
  "organization_id" text NOT NULL,
  "list_version" integer NOT NULL DEFAULT 0,
  "created_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "updated_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  PRIMARY KEY ("organization_id"),
  CONSTRAINT "device_agent_ai_scan_catalogs_organization_id_fkey" FOREIGN KEY ("organization_id") REFERENCES "organization_metadata" ("id") ON UPDATE NO ACTION ON DELETE CASCADE
);
-- Create "device_agent_ai_scan_targets" table
CREATE TABLE "device_agent_ai_scan_targets" (
  "organization_id" text NOT NULL,
  "id" text NOT NULL,
  "display_name" text NOT NULL,
  "category" text NOT NULL,
  "bundle_ids" text[] NOT NULL DEFAULT '{}',
  "binaries" text[] NOT NULL DEFAULT '{}',
  "config_dirs" text[] NOT NULL DEFAULT '{}',
  "process_names" text[] NOT NULL DEFAULT '{}',
  "version_plist_key" text NULL,
  "enabled" boolean NOT NULL DEFAULT true,
  "created_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "updated_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  PRIMARY KEY ("organization_id", "id"),
  CONSTRAINT "device_agent_ai_scan_targets_organization_id_fkey" FOREIGN KEY ("organization_id") REFERENCES "organization_metadata" ("id") ON UPDATE NO ACTION ON DELETE CASCADE
);
