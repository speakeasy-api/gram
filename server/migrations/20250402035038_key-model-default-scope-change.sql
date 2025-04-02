-- Modify "gram_keys" table
ALTER TABLE "gram_keys" ALTER COLUMN "created_by_user_id" SET NOT NULL, ALTER COLUMN "scopes" SET DEFAULT ARRAY['consumer:organization'::text];
