-- ai_integration_syncs holds roughly one row per AI integration config, so
-- this migration runs the add-column/backfill/index-swap sequence in a single
-- transaction instead of a phased expand-contract rollout.

-- Add the schedule discriminator columns as nullable so existing rows can be
-- labeled before the NOT NULL contract is enforced.
ALTER TABLE "ai_integration_syncs" ADD COLUMN "schedule" text NULL, ADD COLUMN "kind" text NULL;

-- Each config's single pre-existing sync row is its provider's primary
-- schedule. Anthropic compliance checkpoints with a pagination cursor; every
-- other schedule checkpoints with the poll watermark.
UPDATE "ai_integration_syncs" AS sync
SET "schedule" = config."provider",
    "kind" = CASE config."provider"
      WHEN 'anthropic_compliance' THEN 'cursor'
      ELSE 'time'
    END,
    "updated_at" = clock_timestamp()
FROM "ai_integration_configs" AS config
WHERE sync."ai_integration_config_id" = config."id";

-- The config-only unique index must go before secondary schedules can be
-- inserted for the same config.
DROP INDEX "ai_integration_syncs_config_id_key";

-- Existing Anthropic configs get the two Admin Analytics schedules that new
-- configs receive on upsert. Epoch means due immediately and doubles as the
-- never-synced watermark sentinel.
INSERT INTO "ai_integration_syncs" (
  "ai_integration_config_id",
  "schedule",
  "kind",
  "poll_watermark_at",
  "next_poll_after"
)
SELECT
  config."id",
  expected."schedule",
  'time',
  TIMESTAMPTZ '1970-01-01 00:00:00+00',
  TIMESTAMPTZ '1970-01-01 00:00:00+00'
FROM "ai_integration_configs" AS config
CROSS JOIN unnest(ARRAY[
  'anthropic_analytics_usage'::text,
  'anthropic_analytics_cost'::text
]) AS expected("schedule")
WHERE config."provider" = 'anthropic_compliance'
  AND config."deleted" IS FALSE;

-- Modify "ai_integration_syncs" table
ALTER TABLE "ai_integration_syncs" ALTER COLUMN "schedule" SET NOT NULL, ALTER COLUMN "kind" SET NOT NULL;
-- Create index "ai_integration_syncs_config_id_schedule_key" to table: "ai_integration_syncs"
CREATE UNIQUE INDEX "ai_integration_syncs_config_id_schedule_key" ON "ai_integration_syncs" ("ai_integration_config_id", "schedule");
