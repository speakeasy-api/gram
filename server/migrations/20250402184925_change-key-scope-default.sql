-- Modify "api_keys" table
ALTER TABLE "api_keys" ALTER COLUMN "scopes" SET DEFAULT ARRAY[]::text[];
