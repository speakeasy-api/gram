-- Create "ai_scan_targets" table
CREATE TABLE "ai_scan_targets" (
  "organization_id" text NOT NULL,
  "id" text NOT NULL,
  "display_name" text NULL,
  "category" text NULL,
  "bundle_ids" text[] NOT NULL DEFAULT '{}',
  "binaries" text[] NOT NULL DEFAULT '{}',
  "config_dirs" text[] NOT NULL DEFAULT '{}',
  "process_names" text[] NOT NULL DEFAULT '{}',
  "version_plist_key" text NULL,
  "cimd_vendor_keys" text[] NOT NULL DEFAULT '{}',
  "oauth_client_ids" text[] NOT NULL DEFAULT '{}',
  "client_info_names" text[] NOT NULL DEFAULT '{}',
  "status" text NOT NULL DEFAULT 'unreviewed',
  "rationale" text NULL,
  "created_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "updated_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  PRIMARY KEY ("organization_id", "id"),
  CONSTRAINT "ai_scan_targets_organization_id_fkey" FOREIGN KEY ("organization_id") REFERENCES "organization_metadata" ("id") ON UPDATE NO ACTION ON DELETE CASCADE
);
