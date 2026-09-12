-- Create "ai_scan_catalogs" table
CREATE TABLE "ai_scan_catalogs" (
  "organization_id" text NOT NULL,
  "list_version" integer NOT NULL DEFAULT 0,
  "created_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "updated_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  PRIMARY KEY ("organization_id"),
  CONSTRAINT "ai_scan_catalogs_organization_id_fkey" FOREIGN KEY ("organization_id") REFERENCES "organization_metadata" ("id") ON UPDATE NO ACTION ON DELETE CASCADE
);
-- Create "ai_scan_targets" table
CREATE TABLE "ai_scan_targets" (
  "organization_id" text NOT NULL,
  "id" text NOT NULL,
  "display_name" text NOT NULL,
  "category" text NOT NULL,
  "bundle_ids" text[] NOT NULL DEFAULT '{}',
  "binaries" text[] NOT NULL DEFAULT '{}',
  "config_dirs" text[] NOT NULL DEFAULT '{}',
  "process_names" text[] NOT NULL DEFAULT '{}',
  "version_plist_key" text NULL,
  "cimd_vendor_keys" text[] NOT NULL DEFAULT '{}',
  "oauth_client_ids" text[] NOT NULL DEFAULT '{}',
  "client_info_names" text[] NOT NULL DEFAULT '{}',
  "enabled" boolean NOT NULL DEFAULT true,
  "created_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "updated_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  PRIMARY KEY ("organization_id", "id"),
  CONSTRAINT "ai_scan_targets_organization_id_fkey" FOREIGN KEY ("organization_id") REFERENCES "organization_metadata" ("id") ON UPDATE NO ACTION ON DELETE CASCADE
);
-- Create "ai_tool_decisions" table
CREATE TABLE "ai_tool_decisions" (
  "organization_id" text NOT NULL,
  "target_id" text NOT NULL,
  "decision" text NOT NULL DEFAULT 'unreviewed',
  "rationale" text NULL,
  "decided_by" text NULL,
  "decided_at" timestamptz NULL,
  "created_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "updated_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  PRIMARY KEY ("organization_id", "target_id"),
  CONSTRAINT "ai_tool_decisions_organization_id_fkey" FOREIGN KEY ("organization_id") REFERENCES "organization_metadata" ("id") ON UPDATE NO ACTION ON DELETE CASCADE,
  CONSTRAINT "ai_tool_decisions_decision_check" CHECK (decision = ANY (ARRAY['unreviewed'::text, 'approved'::text, 'blocked'::text]))
);
