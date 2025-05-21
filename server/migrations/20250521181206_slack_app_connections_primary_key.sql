-- Modify "slack_app_connections" table
ALTER TABLE "slack_app_connections" DROP CONSTRAINT "slack_auth_connections_slack_team_id_key", ADD CONSTRAINT "slack_auth_connections_slack_team_id_key" PRIMARY KEY ("slack_team_id");
