-- Label sync rows created before schedule discriminators were introduced.
UPDATE "ai_integration_syncs" AS sync
SET "schedule" = COALESCE(sync."schedule", config."provider"),
    "kind" = COALESCE(
      sync."kind",
      CASE config."provider"
        WHEN 'anthropic_compliance' THEN 'cursor'
        ELSE 'time'
      END
    ),
    "updated_at" = clock_timestamp()
FROM "ai_integration_configs" AS config
WHERE sync."ai_integration_config_id" = config."id"
  AND (sync."schedule" IS NULL OR sync."kind" IS NULL);

-- Existing Anthropic configs need the two analytics schedules that new writes
-- create through the application.
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
  AND config."deleted" IS FALSE
ON CONFLICT ("ai_integration_config_id", "schedule") DO NOTHING;

-- Modify "ai_integration_syncs" table
ALTER TABLE "ai_integration_syncs" ALTER COLUMN "schedule" SET NOT NULL, ALTER COLUMN "kind" SET NOT NULL;
