-- atlas:txmode none

-- Modify "ai_integration_syncs" table - add discriminator columns as nullable
-- first so the ALTER succeeds on existing rows.
ALTER TABLE "ai_integration_syncs" ADD COLUMN "schedule" text, ADD COLUMN "kind" text;

-- Backfill: every existing row is its config's primary sync, so the schedule
-- label is the config's provider. The kind reflects how each provider's
-- primary sync checkpoints progress: anthropic_compliance resumes from the
-- activities pagination token (cursor), cursor usage polling resumes from
-- poll_watermark_at (time).
UPDATE "ai_integration_syncs" s
SET "schedule" = c."provider",
    "kind" = CASE c."provider"
      WHEN 'anthropic_compliance' THEN 'cursor'
      ELSE 'time'
    END
FROM "ai_integration_configs" c
WHERE c."id" = s."ai_integration_config_id"
  AND s."schedule" IS NULL;

ALTER TABLE "ai_integration_syncs" ALTER COLUMN "schedule" SET NOT NULL;
ALTER TABLE "ai_integration_syncs" ALTER COLUMN "kind" SET NOT NULL;

-- Create index "ai_integration_syncs_config_id_schedule_key" to table: "ai_integration_syncs"
CREATE UNIQUE INDEX CONCURRENTLY "ai_integration_syncs_config_id_schedule_key" ON "ai_integration_syncs" ("ai_integration_config_id", "schedule");
-- Drop index "ai_integration_syncs_config_id_key" from table: "ai_integration_syncs"
DROP INDEX CONCURRENTLY "ai_integration_syncs_config_id_key";
