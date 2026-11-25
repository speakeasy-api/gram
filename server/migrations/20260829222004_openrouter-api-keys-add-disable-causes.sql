-- Modify "openrouter_api_keys" table
ALTER TABLE "openrouter_api_keys" ADD COLUMN "disable_causes" text[] NULL;
