-- Modify "api_keys" table
ALTER TABLE "api_keys" ADD COLUMN "last_accessed_at" timestamptz NULL;
