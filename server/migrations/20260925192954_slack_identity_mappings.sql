-- Create "slack_directory_connections" table
CREATE TABLE "slack_directory_connections" (
  "id" uuid NOT NULL DEFAULT generate_uuidv7(),
  "organization_id" text NOT NULL,
  "slack_team_id" text NOT NULL,
  "slack_team_name" text NULL,
  "credentials_encrypted" text NULL,
  "granted_scopes" text[] NOT NULL DEFAULT ARRAY[]::text[],
  "generation" uuid NOT NULL DEFAULT generate_uuidv7(),
  "health" text NOT NULL DEFAULT 'pending',
  "disconnected_at" timestamptz NULL,
  "last_sync_started_at" timestamptz NULL,
  "last_full_sync_generation" uuid NULL,
  "last_full_sync_succeeded_at" timestamptz NULL,
  "last_sync_failed_at" timestamptz NULL,
  "last_error_code" text NULL,
  "created_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "updated_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  PRIMARY KEY ("id"),
  CONSTRAINT "slack_directory_connections_organization_id_fkey" FOREIGN KEY ("organization_id") REFERENCES "organization_metadata" ("id") ON UPDATE NO ACTION ON DELETE SET NULL
);
-- Create index "slack_directory_connections_org_team_key" to table: "slack_directory_connections"
CREATE UNIQUE INDEX "slack_directory_connections_org_team_key" ON "slack_directory_connections" ("organization_id", "slack_team_id");
-- Create "slack_directory_memberships" table
CREATE TABLE "slack_directory_memberships" (
  "id" uuid NOT NULL DEFAULT generate_uuidv7(),
  "organization_id" text NOT NULL,
  "slack_team_id" text NOT NULL,
  "slack_user_id" text NOT NULL,
  "display_name" text NULL,
  "email" text NULL,
  "status" text NOT NULL DEFAULT 'unknown',
  "member_type" text NOT NULL DEFAULT 'unknown',
  "provider_updated_at" timestamptz NULL,
  "last_seen_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "mapping_revision" bigint NOT NULL DEFAULT 0,
  "mapping_conflict_reason" text NULL,
  "mapping_conflict_detected_at" timestamptz NULL,
  "created_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "updated_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  PRIMARY KEY ("id"),
  CONSTRAINT "slack_directory_memberships_connection_fkey" FOREIGN KEY ("organization_id", "slack_team_id") REFERENCES "slack_directory_connections" ("organization_id", "slack_team_id") ON UPDATE NO ACTION ON DELETE SET NULL
);
-- Create index "slack_directory_memberships_org_team_user_key" to table: "slack_directory_memberships"
CREATE UNIQUE INDEX "slack_directory_memberships_org_team_user_key" ON "slack_directory_memberships" ("organization_id", "slack_team_id", "slack_user_id");
-- Create "slack_identity_mappings" table
CREATE TABLE "slack_identity_mappings" (
  "id" uuid NOT NULL DEFAULT generate_uuidv7(),
  "organization_id" text NOT NULL,
  "slack_team_id" text NOT NULL,
  "slack_user_id" text NOT NULL,
  "user_id" text NOT NULL,
  "created_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "updated_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "revoked_at" timestamptz NULL,
  PRIMARY KEY ("id"),
  CONSTRAINT "slack_identity_mappings_membership_fkey" FOREIGN KEY ("organization_id", "slack_team_id", "slack_user_id") REFERENCES "slack_directory_memberships" ("organization_id", "slack_team_id", "slack_user_id") ON UPDATE NO ACTION ON DELETE SET NULL,
  CONSTRAINT "slack_identity_mappings_organization_id_user_id_fkey" FOREIGN KEY ("organization_id", "user_id") REFERENCES "organization_user_relationships" ("organization_id", "user_id") ON UPDATE NO ACTION ON DELETE SET NULL,
  CONSTRAINT "slack_identity_mappings_user_id_fkey" FOREIGN KEY ("user_id") REFERENCES "users" ("id") ON UPDATE NO ACTION ON DELETE SET NULL
);
-- Create index "slack_identity_mappings_current_key" to table: "slack_identity_mappings"
CREATE UNIQUE INDEX "slack_identity_mappings_current_key" ON "slack_identity_mappings" ("organization_id", "slack_team_id", "slack_user_id") WHERE (revoked_at IS NULL);
-- Create index "slack_identity_mappings_membership_history_idx" to table: "slack_identity_mappings"
CREATE INDEX "slack_identity_mappings_membership_history_idx" ON "slack_identity_mappings" ("organization_id", "slack_team_id", "slack_user_id", "created_at" DESC);
-- Create index "slack_identity_mappings_user_org_idx" to table: "slack_identity_mappings"
CREATE INDEX "slack_identity_mappings_user_org_idx" ON "slack_identity_mappings" ("user_id", "organization_id");
