-- Rename a column from "last_polled_at" to "poll_watermark_at"
ALTER TABLE "ai_integration_syncs" RENAME COLUMN "last_polled_at" TO "poll_watermark_at";
-- Modify "ai_integration_syncs" table
ALTER TABLE "ai_integration_syncs" ADD COLUMN "next_poll_after" timestamptz NOT NULL DEFAULT clock_timestamp(), ADD COLUMN "last_poll_error" text NULL, ADD COLUMN "last_poll_failed_at" timestamptz NULL, ADD COLUMN "last_poll_success_at" timestamptz NULL, ADD COLUMN "consecutive_failures" integer NOT NULL DEFAULT 0;
