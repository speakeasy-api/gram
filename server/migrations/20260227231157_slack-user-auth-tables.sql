-- atlas:nolint DS102
-- Create "slack_pending_auths" table
CREATE TABLE "slack_pending_auths" (
  "id" uuid NOT NULL DEFAULT gen_random_uuid(),
  "slack_app_id" uuid NOT NULL,
  "slack_user_id" text NOT NULL,
  "token" text NOT NULL,
  "channel_id" text NOT NULL,
  "status" text NOT NULL DEFAULT 'pending',
  "created_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "completed_at" timestamptz NULL,
  PRIMARY KEY ("id"),
  CONSTRAINT "slack_pending_auths_token_key" UNIQUE ("token"),
  CONSTRAINT "slack_pending_auths_slack_app_id_fkey" FOREIGN KEY ("slack_app_id") REFERENCES "slack_apps" ("id") ON UPDATE NO ACTION ON DELETE CASCADE
);
-- Create "slack_user_mappings" table
CREATE TABLE "slack_user_mappings" (
  "created_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "slack_user_id" text NOT NULL,
  "id" uuid NOT NULL DEFAULT gen_random_uuid(),
  "slack_app_id" uuid NOT NULL,
  "gram_user_id" uuid NOT NULL,
  PRIMARY KEY ("id"),
  CONSTRAINT "slack_user_mappings_slack_app_id_slack_user_id_key" UNIQUE ("slack_app_id", "slack_user_id"),
  CONSTRAINT "slack_user_mappings_slack_app_id_fkey" FOREIGN KEY ("slack_app_id") REFERENCES "slack_apps" ("id") ON UPDATE NO ACTION ON DELETE CASCADE
);
-- Drop "slack_app_connections" table
DROP TABLE "slack_app_connections";
