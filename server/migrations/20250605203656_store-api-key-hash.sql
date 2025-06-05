-- Modify "api_keys" table
ALTER TABLE "api_keys" DROP COLUMN "token", ADD COLUMN "key_prefix" text NOT NULL, ADD COLUMN "key_hash" text NOT NULL, ADD CONSTRAINT "api_keys_key_hash" UNIQUE ("key_hash");
