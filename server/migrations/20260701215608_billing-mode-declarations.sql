-- Modify "ai_integration_configs" table
ALTER TABLE "ai_integration_configs" ADD COLUMN "billing_mode" text NULL;
-- Modify "user_accounts" table
ALTER TABLE "user_accounts" ADD COLUMN "billing_mode" text NULL, ADD COLUMN "plan_type" text NULL;
