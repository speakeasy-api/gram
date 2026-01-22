-- Modify "environments" table
ALTER TABLE "environments" ADD COLUMN "entry_display_names" jsonb NOT NULL DEFAULT '{}';
