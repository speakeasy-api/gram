-- atlas:txmode none

-- Drop index "slack_apps_slack_team_id_key" from table: "slack_apps"
DROP INDEX CONCURRENTLY "slack_apps_slack_team_id_key";
-- Create index "slack_apps_slack_team_id_idx" to table: "slack_apps"
CREATE INDEX CONCURRENTLY "slack_apps_slack_team_id_idx" ON "slack_apps" ("slack_team_id") WHERE ((deleted IS FALSE) AND (slack_team_id IS NOT NULL));
-- Create "slack_registrations" table
CREATE TABLE "slack_registrations" (
  "id" uuid NOT NULL DEFAULT generate_uuidv7(),
  "slack_app_id" uuid NOT NULL,
  "slack_account_id" text NOT NULL,
  "user_id" uuid NOT NULL,
  "created_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "updated_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  PRIMARY KEY ("id"),
  CONSTRAINT "slack_registrations_slack_app_id_slack_account_id_key" UNIQUE ("slack_app_id", "slack_account_id"),
  CONSTRAINT "slack_registrations_slack_app_id_fkey" FOREIGN KEY ("slack_app_id") REFERENCES "slack_apps" ("id") ON UPDATE NO ACTION ON DELETE CASCADE
);
-- Drop "slack_app_connections" table
DROP TABLE "slack_app_connections";
