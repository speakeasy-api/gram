-- Rename the completed-query cursor away from scheduler terminology.
ALTER TABLE "ai_integration_syncs"
  RENAME COLUMN "last_polled_at" TO "poll_watermark_at";

-- Track scheduler eligibility independently from the query cursor plus
-- failure state for observability. Non-volatile defaults avoid a table
-- rewrite when adding NOT NULL columns to existing rows; the runtime
-- default is set on a subsequent ALTER COLUMN.
ALTER TABLE "ai_integration_syncs"
  ADD COLUMN "next_poll_after" timestamptz NOT NULL DEFAULT 'epoch',
  ADD COLUMN "last_poll_error" text,
  ADD COLUMN "last_poll_failed_at" timestamptz,
  ADD COLUMN "last_poll_success_at" timestamptz,
  ADD COLUMN "consecutive_failures" integer NOT NULL DEFAULT 0;

-- Existing rows should preserve the old hourly cadence after the split.
UPDATE "ai_integration_syncs"
SET "next_poll_after" = "poll_watermark_at" + INTERVAL '1 hour';

-- Future inserts use clock_timestamp() so newly-onboarded configs are
-- immediately eligible to poll.
ALTER TABLE "ai_integration_syncs"
  ALTER COLUMN "next_poll_after" SET DEFAULT clock_timestamp();
