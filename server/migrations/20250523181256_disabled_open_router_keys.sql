-- Modify "openrouter_api_keys" table
ALTER TABLE "openrouter_api_keys" ADD COLUMN "disabled" boolean NOT NULL DEFAULT false;
