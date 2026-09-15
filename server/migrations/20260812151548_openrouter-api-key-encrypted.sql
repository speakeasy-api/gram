-- Modify "openrouter_api_keys" table
ALTER TABLE "openrouter_api_keys" ALTER COLUMN "key" DROP NOT NULL, ADD COLUMN "key_encrypted" text NULL;
