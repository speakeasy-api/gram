-- Modify "openrouter_api_keys" table
ALTER TABLE "openrouter_api_keys" ADD COLUMN "disable_causes" text[] NOT NULL DEFAULT '{}';

UPDATE "openrouter_api_keys"
SET "disable_causes" = ARRAY['admin_lock']::text[]
WHERE "disabled" IS TRUE;

ALTER TABLE "openrouter_api_keys" DROP COLUMN "disabled", ADD COLUMN "disabled" boolean NOT NULL GENERATED ALWAYS AS (cardinality(disable_causes) > 0) STORED;
