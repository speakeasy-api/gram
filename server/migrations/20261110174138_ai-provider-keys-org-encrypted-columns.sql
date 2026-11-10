-- Modify "ai_integration_configs" table
ALTER TABLE "ai_integration_configs" ADD COLUMN "api_key_org_encrypted" text NULL;
-- Set comment to column: "api_key_org_encrypted" on table: "ai_integration_configs"
COMMENT ON COLUMN "ai_integration_configs"."api_key_org_encrypted" IS '@access: secret-restricted';
-- Modify "model_provider_keys" table
ALTER TABLE "model_provider_keys" ADD COLUMN "api_key_org_encrypted" text NULL;
-- Set comment to column: "api_key_org_encrypted" on table: "model_provider_keys"
COMMENT ON COLUMN "model_provider_keys"."api_key_org_encrypted" IS '@access: secret-restricted';
-- Modify "openrouter_api_keys" table
ALTER TABLE "openrouter_api_keys" ADD COLUMN "key_org_encrypted" text NULL;
-- Set comment to column: "key_org_encrypted" on table: "openrouter_api_keys"
COMMENT ON COLUMN "openrouter_api_keys"."key_org_encrypted" IS '@access: secret-restricted';
