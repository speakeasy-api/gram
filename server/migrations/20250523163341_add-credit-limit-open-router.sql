-- Modify "openrouter_api_keys" table
ALTER TABLE "openrouter_api_keys" ADD COLUMN "monthly_credits" bigint NOT NULL DEFAULT 0;
