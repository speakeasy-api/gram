-- Create "ai_integration_analytics_syncs" table
CREATE TABLE "ai_integration_analytics_syncs" (
  "created_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "updated_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "ai_integration_config_id" uuid NOT NULL,
  "poll_watermark_at" timestamptz NULL,
  "next_poll_after" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "last_poll_error" text NULL,
  "last_poll_failed_at" timestamptz NULL,
  "last_poll_success_at" timestamptz NULL,
  "consecutive_failures" integer NOT NULL DEFAULT 0,
  "id" uuid NOT NULL DEFAULT generate_uuidv7(),
  PRIMARY KEY ("id"),
  CONSTRAINT "ai_integration_analytics_syncs_config_id_fkey" FOREIGN KEY ("ai_integration_config_id") REFERENCES "ai_integration_configs" ("id") ON UPDATE NO ACTION ON DELETE CASCADE
);
-- Create index "ai_integration_analytics_syncs_config_id_key" to table: "ai_integration_analytics_syncs"
CREATE UNIQUE INDEX "ai_integration_analytics_syncs_config_id_key" ON "ai_integration_analytics_syncs" ("ai_integration_config_id");
