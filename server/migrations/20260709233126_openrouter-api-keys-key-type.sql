-- Modify "openrouter_api_keys" table
ALTER TABLE "openrouter_api_keys" DROP CONSTRAINT "openrouter_api_keys_pkey", ADD COLUMN "key_type" text NOT NULL DEFAULT 'chat', ADD PRIMARY KEY ("organization_id", "key_type");
