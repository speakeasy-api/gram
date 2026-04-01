-- Modify "organization_metadata" table
ALTER TABLE "organization_metadata" ADD COLUMN "free_trial_started_at" timestamptz NOT NULL DEFAULT clock_timestamp(), ADD COLUMN "free_trial_ends_at" timestamptz NOT NULL DEFAULT (clock_timestamp() + '14 days'::interval);
